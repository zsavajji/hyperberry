package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"google.golang.org/protobuf/proto"
)

// Stickers: the favourites the phone keeps in app state, the ones we sent, and sending one again. A sticker is
// named by the base64 SHA-256 of its file, as app state names it (messages.sticker, sticker_faves.hash). Nothing is
// uploaded: a sticker goes out pointing at the upload already on the server, as a forward does.

// lottieMime is the mimetype of an animated Lottie sticker, which the UI plays itself.
const lottieMime = "application/was"

// stickerHost is what a favourite's URL starts with; the rest is the direct path.
const stickerHost = "https://mmg.whatsapp.net"

func (m mediaInfo) lottie() bool { return m.Mimetype == lottieMime }

// stickerSrc is what sending or favouriting a sticker needs of it.
type stickerSrc struct {
	info     mediaInfo
	w, h     int
	thumb    []byte // a message row's PNG; a favourite has none
	animated bool
	path     string // the file, when it is here
}

// stickerItem is one sticker of the stickers.get lists.
type stickerItem struct {
	Hash   string `json:"hash"`
	W      int    `json:"w"`
	H      int    `json:"h"`
	Path   string `json:"path"`
	Lottie bool   `json:"lottie,omitempty"`
}

// stickerCopy is where stickers.file keeps a sticker's file, named by the hex of its hash: .was for a Lottie
// sticker, else .webp.
func stickerCopy(dir, hash string, lottie bool) string {
	sum, _ := base64.StdEncoding.DecodeString(hash)
	ext := ".webp"
	if lottie {
		ext = ".was"
	}
	return filepath.Join(dir, "stickers", hex.EncodeToString(sum)+ext)
}

// isAnimatedWebP reads the first 21 bytes of a WebP file: "RIFF", its size, "WEBP", "VP8X", that chunk's size, then
// the flags, of which 0x02 is animation.
func isAnimatedWebP(head []byte) bool {
	return len(head) >= 21 && string(head[:4]) == "RIFF" && string(head[8:16]) == "WEBPVP8X" && head[20]&0x02 != 0
}

func fileAnimated(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 21)
	n, _ := io.ReadFull(f, head)
	return isAnimatedWebP(head[:n])
}

// setFave stores or drops one of the phone's favourite stickers, from app state or from our own sticker.fave: the
// action's IsFavorite says which. The messages that carry the sticker are told when it appeared or went, so their cards
// read their fave flag again.
func (t *storeTx) setFave(hash string, a *waSyncAction.StickerAction, ts int64) {
	sum, err := base64.StdEncoding.DecodeString(hash)
	if err != nil || len(sum) == 0 {
		return
	}
	var one int
	had := t.tx.QueryRow("SELECT 1 FROM sticker_faves WHERE hash = ?", hash).Scan(&one) == nil
	on := a.GetIsFavorite()
	if on {
		media, _ := json.Marshal(mediaInfo{a.GetDirectPath(), a.GetMediaKey(), a.GetFileEncSHA256(), sum, a.GetFileLength(), a.GetMimetype()})
		t.exec("INSERT OR REPLACE INTO sticker_faves(hash, media, width, height, ts) VALUES(?, ?, ?, ?, ?)",
			hash, string(media), a.GetWidth(), a.GetHeight(), ts)
	} else {
		t.exec("DELETE FROM sticker_faves WHERE hash = ?", hash)
	}
	if on == had {
		return
	}
	rows, err := t.tx.Query("SELECT chat, id FROM messages WHERE sticker = ?", hash)
	if err != nil {
		t.log.Warnf("fave: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var chat, id string
		if rows.Scan(&chat, &id) == nil {
			t.changed(chat, id)
		}
	}
}

// stickerFile is where a sticker's file is here, "" when it isn't: a message's download, else stickers.file's copy.
func (t *storeTx) stickerFile(dir, hash string, lottie bool) string {
	var paths []string
	if rows, err := t.tx.Query("SELECT path FROM messages WHERE sticker = ? AND media_state = 2 AND path IS NOT NULL ORDER BY ts DESC", hash); err == nil {
		for rows.Next() {
			var p string
			if rows.Scan(&p) == nil {
				paths = append(paths, p)
			}
		}
		rows.Close()
	}
	for _, p := range append(paths, stickerCopy(dir, hash, lottie)) {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// stickerList runs q, whose rows are a hash, its media, its width and height, and a time to order by (unused here).
func (t *storeTx) stickerList(dir, q string) []stickerItem {
	list := []stickerItem{}
	rows, err := t.tx.Query(q)
	if err != nil {
		t.log.Warnf("stickers: %v", err)
		return list
	}
	for rows.Next() {
		var it stickerItem
		var media string
		var ts int64
		if rows.Scan(&it.Hash, &media, &it.W, &it.H, &ts) == nil {
			var info mediaInfo
			json.Unmarshal([]byte(media), &info) // our own marshal: it reads
			it.Lottie = info.lottie()
			list = append(list, it)
		}
	}
	rows.Close() // before the lookups below
	usable := list[:0]
	for _, it := range list {
		// A sticker with no file here and nothing to fetch it with would be an empty cell, and can't be sent.
		if src, ok := t.stickerSource(dir, it.Hash); ok && (src.path != "" || src.usable()) {
			it.Path = src.path
			usable = append(usable, it)
		}
	}
	return usable
}

// stickerLists are the favourites, newest first, and the 32 stickers we sent most recently, as WhatsApp's own Recent tab.
func (t *storeTx) stickerLists(dir string) (faves, recent []stickerItem) {
	faves = t.stickerList(dir, "SELECT hash, media, COALESCE(width, 0), COALESCE(height, 0), ts FROM sticker_faves ORDER BY ts DESC")
	// The bare columns are the newest row's, as SQLite has them with one MAX.
	recent = t.stickerList(dir, `SELECT sticker, media, COALESCE(width, 0), COALESCE(height, 0), MAX(ts) FROM messages
WHERE kind = 5 AND out = 1 AND sticker IS NOT NULL AND deleted = 0 GROUP BY sticker ORDER BY 5 DESC LIMIT 32`)
	return faves, recent
}

// stickerSource finds a sticker's details: its favourite's, else the newest message's that carries it. The phone's app
// state has favourites with no media details at all, the hash alone (2026-11-22: three of seven, the older ones); then a
// message's details stand in. ok is false when neither is here; usable() says whether it can be fetched or sent.
func (t *storeTx) stickerSource(dir, hash string) (src stickerSrc, ok bool) {
	var media, rowMedia string
	var rowW, rowH int
	var extra sql.NullString
	fave := t.tx.QueryRow("SELECT media, COALESCE(width, 0), COALESCE(height, 0) FROM sticker_faves WHERE hash = ?", hash).
		Scan(&media, &src.w, &src.h) == nil
	inRow := t.tx.QueryRow(`SELECT media, COALESCE(width, 0), COALESCE(height, 0), thumb, extra FROM messages
WHERE sticker = ? AND deleted = 0 AND media IS NOT NULL AND json_extract(media, '$.directPath') != ''
ORDER BY ts DESC LIMIT 1`, hash).
		Scan(&rowMedia, &rowW, &rowH, &src.thumb, &extra) == nil
	if !fave && !inRow {
		return src, false
	}
	json.Unmarshal([]byte(media), &src.info) // our own marshal: it reads ("" when no favourite)
	if inRow && !src.usable() {
		json.Unmarshal([]byte(rowMedia), &src.info)
		if rowW > 0 && rowH > 0 {
			src.w, src.h = rowW, rowH
		}
	}
	var x struct{ Animated bool }
	json.Unmarshal([]byte(extra.String), &x) // no extra, or no such key: not marked
	src.animated = x.Animated || src.info.lottie()
	if src.path = t.stickerFile(dir, hash, src.info.lottie()); !src.animated && src.path != "" {
		src.animated = fileAnimated(src.path)
	}
	return src, true
}

// usable is whether src points at an upload: what a download or a send needs.
func (src stickerSrc) usable() bool { return src.info.DirectPath != "" && len(src.info.MediaKey) > 0 }

// stickerOut is the message that sends src and our row of it, as queueRow takes it. The quote's ContextInfo is the
// caller's, once it has the quoted message: on flatten(msg).GetStickerMessage(), wrapped or not.
func stickerOut(src stickerSrc, hash, chat, id string, now int64) (*waE2E.Message, msgRow) {
	i := src.info
	d := i.message(msgSticker).(*waE2E.StickerMessage)
	d.PngThumbnail, d.IsAnimated, d.IsLottie = src.thumb, proto.Bool(src.animated), proto.Bool(i.lottie())
	if src.w > 0 && src.h > 0 {
		d.Width, d.Height = proto.Uint32(uint32(src.w)), proto.Uint32(uint32(src.h))
	}
	media, _ := json.Marshal(i)
	row := msgRow{Chat: chat, ID: id, TS: now, Out: true, State: stateSending, Kind: msgSticker, Sticker: hash,
		Media: string(media), MediaState: 1, Size: int64(i.FileLength), Thumb: src.thumb, Width: src.w, Height: src.h}
	if src.path != "" {
		row.Path, row.MediaState = src.path, 2
	}
	if src.animated {
		row.Extra = withKey("", "animated", "true")
	}
	msg := &waE2E.Message{StickerMessage: d}
	if i.lottie() { // WhatsApp wraps a Lottie sticker, as a received one comes (msgbiz.go's unwrap)
		msg = &waE2E.Message{LottieStickerMessage: &waE2E.FutureProofMessage{Message: msg}}
	}
	return msg, row
}

// stickerAction is the app-state value that favourites src, or takes it off.
func stickerAction(src stickerSrc, on bool) *waSyncAction.StickerAction {
	i := src.info
	a := &waSyncAction.StickerAction{URL: proto.String(stickerHost + i.DirectPath), DirectPath: &i.DirectPath,
		MediaKey: i.MediaKey, FileEncSHA256: i.FileEncSHA256, Mimetype: &i.Mimetype, FileLength: &i.FileLength,
		IsFavorite: proto.Bool(on), IsLottie: proto.Bool(i.lottie())}
	if src.w > 0 && src.h > 0 {
		a.Width, a.Height = proto.Uint32(uint32(src.w)), proto.Uint32(uint32(src.h))
	}
	return a
}

func registerStickers(srv *Server, s *Session) {
	srv.Handle("stickers.get", s.handleStickersGet)
	srv.HandleAsync("stickers.file", s.handleStickersFile) // a download: its own goroutine
	srv.Handle("sticker.send", s.handleStickerSend)
	srv.Handle("sticker.fave", s.handleStickerFave)
}

// handleStickersGet is stickers.get: the lists, each sticker with the path of its file when that is here.
func (s *Session) handleStickersGet(json.RawMessage) (any, error) {
	dir, err := filepath.Abs(s.srv.dir)
	if err != nil {
		return nil, err
	}
	var faves, recent []stickerItem
	if err := s.store.runOp(func(t *storeTx) { faves, recent = t.stickerLists(dir) }); err != nil {
		return nil, err
	}
	return map[string]any{"faves": faves, "recent": recent}, nil
}

// handleStickersFile is stickers.file: the path of a sticker's file, downloaded to stickers/ when no message's is here.
func (s *Session) handleStickersFile(req json.RawMessage) (any, error) {
	var r struct{ Hash string }
	if err := json.Unmarshal(req, &r); err != nil || r.Hash == "" {
		return nil, &Error{"bad_request", "stickers.file needs hash"}
	}
	cli, err := s.linkedClient()
	if err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(s.srv.dir)
	if err != nil {
		return nil, err
	}
	// One download of a sticker at a time, as a message's has (fetchMedia): the second finds the file.
	mu, _ := s.store.dl.LoadOrStore("sticker/"+r.Hash, new(sync.Mutex))
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	var src stickerSrc
	var ok bool
	if err := s.store.runOp(func(t *storeTx) { src, ok = t.stickerSource(dir, r.Hash) }); err != nil {
		return nil, err
	}
	switch {
	case !ok:
		return nil, &Error{"not_found", "no such sticker"}
	case src.path != "":
		return map[string]string{"path": src.path}, nil
	case !src.usable():
		return nil, &Error{"not_found", "this sticker's file isn't on WhatsApp's servers"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	path := stickerCopy(dir, r.Hash, src.info.lottie())
	data, err := cli.Download(ctx, src.info.message(msgSticker))
	if err == nil {
		err = writeFile(path, data)
	}
	if err != nil {
		return nil, &Error{"download_failed", err.Error()}
	}
	return map[string]string{"path": path}, nil
}

// handleStickerSend is sticker.send: the row is in as sending, then the message goes, as handleForward sends one.
func (s *Session) handleStickerSend(req json.RawMessage) (any, error) {
	var r struct{ Chat, Hash, Quote string }
	if err := json.Unmarshal(req, &r); err != nil || r.Chat == "" || r.Hash == "" {
		return nil, &Error{"bad_request", "sticker.send needs chat and hash"}
	}
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(s.srv.dir)
	if err != nil {
		return nil, err
	}
	var src stickerSrc
	var ok bool
	if err := s.store.runOp(func(t *storeTx) { src, ok = t.stickerSource(dir, r.Hash) }); err != nil {
		return nil, err
	}
	if !ok {
		return nil, &Error{"not_found", "no such sticker"}
	}
	if !src.usable() { // the server takes a sticker with no upload, and nobody gets anything
		return nil, &Error{"bad_request", "this sticker's file isn't on WhatsApp's servers"}
	}
	id := cli.GenerateMessageID()
	msg, row := stickerOut(src, r.Hash, r.Chat, id, time.Now().Unix())
	q, created := s.store.queueRow(jid, r.Chat, r.Quote, row)
	s.nameNew(cli, jid, r.Chat, created)
	if q.ID != "" {
		flatten(msg).GetStickerMessage().ContextInfo = quoteInfo(cli, r.Chat, q)
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	resp, err := cli.SendMessage(ctx, jid, msg, whatsmeow.SendRequestExtra{ID: id})
	if err != nil {
		s.store.ops <- func(t *storeTx) { t.sentAs(r.Chat, id, 0, stateFailed) }
		return nil, err
	}
	s.store.ops <- func(t *storeTx) { t.sentAs(r.Chat, id, resp.Timestamp.Unix(), stateSent) }
	return map[string]string{"id": id}, nil
}

// handleStickerFave is sticker.fave: a sticker by its hash, or by the message that carries it, becomes a favourite or
// stops being one. It changes here at once, since WhatsApp doesn't echo our own patch to this device, then the main phone
// is told.
func (s *Session) handleStickerFave(req json.RawMessage) (any, error) {
	var r struct {
		Hash, Chat, ID string
		On             *bool
	}
	if err := json.Unmarshal(req, &r); err != nil || r.On == nil || (r.Hash == "" && (r.Chat == "" || r.ID == "")) {
		return nil, &Error{"bad_request", "sticker.fave needs on, and hash or chat and id"}
	}
	cli, err := s.linkedClient()
	if err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(s.srv.dir)
	if err != nil {
		return nil, err
	}
	hash := r.Hash
	if hash == "" {
		row, ok := s.store.row(r.Chat, r.ID)
		if !ok || row.Sticker == "" {
			return nil, &Error{"not_found", "no such sticker"}
		}
		hash = row.Sticker
	}
	var src stickerSrc
	var ok bool
	if err := s.store.runOp(func(t *storeTx) { src, ok = t.stickerSource(dir, hash) }); err != nil {
		return nil, err
	}
	if !ok {
		return nil, &Error{"not_found", "no such sticker"}
	}
	a := stickerAction(src, *r.On)
	sum, _ := base64.StdEncoding.DecodeString(hash)
	patch, err := appstate.BuildFavoriteSticker(sum, a) // checks the hash and the media details before anything changes
	if err != nil {
		return nil, &Error{"bad_request", err.Error()}
	}
	s.store.ops <- func(t *storeTx) { t.setFave(hash, a, time.Now().Unix()) }
	s.sendPatch(cli, "sticker.fave", patch)
	return nil, nil
}
