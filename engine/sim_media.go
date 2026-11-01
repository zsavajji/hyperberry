package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"image/jpeg"
	"strings"
	"time"
)

// The sim's photos, documents and audio files (step 6.1): its history's media rows carry a
// thumbnail, size and media_state 1, and media.get makes the file when asked, as a download.

const (
	simPhotoW, simPhotoH = 1600, 1200
	simAudioChat         = 7  // Elena's history has the one audio file
	simAudioIndex        = 5  // ... as its sixth message
	simAudioSecs         = 20 // ... and it plays this long
)

func (s *Sim) registerSimMedia() {
	s.srv.Handle("sim.photo", s.handlePhoto)
	s.srv.Handle("media.get", s.handleMediaGet)
}

// simAudio turns a generated history row into the incoming audio file.
func simAudio(r *msgRow) {
	r.Kind, r.Text, r.Out, r.Duration = msgAudio, "", false, simAudioSecs
}

func simID(now time.Time) string { return fmt.Sprintf("SIM%d", now.UnixNano()) }

func simHash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// simGradient is a w by h picture whose colours come from id: a diagonal blend between two.
func simGradient(w, h int, id string) *image.RGBA {
	k := simHash(id)
	c0 := [3]int{int(k & 0xff), int(k >> 8 & 0xff), int(k >> 16 & 0xff)}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t, i := 255*(x+y)/(w+h), img.PixOffset(x, y)
			for c := 0; c < 3; c++ {
				img.Pix[i+c] = uint8(c0[c] + (255-2*c0[c])*t/255)
			}
			img.Pix[i+3] = 255
		}
	}
	return img
}

func simJPEG(img image.Image, quality int) []byte {
	var b bytes.Buffer
	jpeg.Encode(&b, img, &jpeg.Options{Quality: quality})
	return b.Bytes()
}

// simThumb is the inline thumbnail of message id.
func simThumb(id string) []byte { return simJPEG(simGradient(80, 60, id), 70) }

// simPDF is a valid one-page PDF that says "HyperBerry test".
var simPDF = func() []byte {
	text := "BT /F1 24 Tf 72 720 Td (HyperBerry test) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(text), text),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	at := make([]int, len(objs))
	for i, o := range objs {
		at[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, a := range at {
		fmt.Fprintf(&b, "%010d 00000 n \n", a)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}()

// seedMedia gives a seeded row its audio (voice notes: here already) or, for a photo, document
// or audio file, what a history row has before the file comes: size, thumbnail, state 1.
func (s *Sim) seedMedia(r *msgRow) {
	s.voiceAudio(r)
	switch r.Kind {
	case msgPhoto:
		r.Width, r.Height, r.Size = simPhotoW, simPhotoH, 180000+int64(simHash(r.ID)%120000)
	case msgDoc:
		r.Size = int64(len(simPDF))
	case msgAudio:
		r.Size = int64(44 + 2*simRate*r.Duration)
	default:
		return
	}
	r.MediaState = 1
	if r.Kind != msgAudio {
		r.Thumb = simThumb(r.ID)
	}
}

// simFile is the download: a photo, a PDF or a WAV by the row's kind, else an error.
func simFile(row msgRow) ([]byte, string, error) {
	switch {
	case row.Kind == msgPhoto:
		return simJPEG(simGradient(simPhotoW, simPhotoH, row.ID), 85), "image/jpeg", nil
	case row.Kind == msgDoc && strings.HasSuffix(strings.ToLower(row.Text), ".pdf"):
		return simPDF, "application/pdf", nil
	case row.Kind == msgAudio:
		return writeWAV(simSpeech(max(row.Duration, 1)), simRate), "audio/wav", nil
	}
	return nil, "", errors.New("the simulated account has no such file")
}

// fetchMedia is the sim's download: a round trip, then the file as the real one stores it.
func (s *Sim) fetchMedia(row msgRow) (string, error) {
	return s.store.fetchMedia(s.srv.dir, row, func() ([]byte, string, error) {
		time.Sleep(simDelay)
		return simFile(row)
	})
}

func (s *Sim) handleMediaGet(req json.RawMessage) (any, error) {
	r, err := parseMediaGet(req)
	if err != nil {
		return nil, err
	}
	if _, err := s.chat(r.Chat); err != nil {
		return nil, err
	}
	return s.store.mediaGet(s.srv.dir, r, s.fetchMedia)
}

// handlePhoto is sim.photo: an incoming photo in chat n, now, on the server; it downloads at
// once, as a live real photo does.
func (s *Sim) handlePhoto(req json.RawMessage) (any, error) {
	var r struct{ N int }
	json.Unmarshal(req, &r)
	if err := s.ready(); err != nil {
		return nil, err
	}
	if r.N < 0 || r.N >= len(simChats) {
		return nil, &Error{"bad_request", fmt.Sprintf("n is 0 to %d", len(simChats)-1)}
	}
	c, now := simChats[r.N], time.Now()
	id := simID(now)
	m := msgRow{Kind: msgPhoto, Text: "A photo from the sim 📷", Width: simPhotoW, Height: simPhotoH,
		Size: 180000 + int64(simHash(id)%120000), Thumb: simThumb(id), MediaState: 1}
	s.deliver(c, now, m)
	m.Chat, m.ID = c.jid, id
	go func() {
		if _, err := s.fetchMedia(m); err != nil {
			s.log.Warnf("sim photo %s: %v", id, err)
		}
	}()
	return nil, nil
}
