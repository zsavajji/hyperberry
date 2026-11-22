package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

const stickerChat = "4915700000002@s.whatsapp.net"

// hashOf is a sticker's name: the base64 of 32 bytes that are all b.
func hashOf(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }

// infoOf is the download details of the sticker hashOf(b) names.
func infoOf(b byte, mimeType string) mediaInfo {
	return mediaInfo{DirectPath: fmt.Sprintf("/v/s%d", b), MediaKey: bytes.Repeat([]byte{b}, 32),
		FileEncSHA256: bytes.Repeat([]byte{b + 1}, 32), FileSHA256: bytes.Repeat([]byte{b}, 32), FileLength: 900, Mimetype: mimeType}
}

// stickerRow is a sticker message as fillMedia keeps one.
func stickerRow(id string, ts int64, out bool, b byte, mimeType string) msgRow {
	media, _ := json.Marshal(infoOf(b, mimeType))
	return msgRow{Chat: stickerChat, ID: id, TS: ts, Out: out, State: stateSent, Kind: msgSticker, Media: string(media),
		MediaState: 1, Size: 900, Width: 512, Height: 512, Sticker: hashOf(b)}
}

// faveOf is the app-state action that makes the sticker hashOf(b) a favourite, or not.
func faveOf(b byte, mimeType string, on bool) *waSyncAction.StickerAction {
	i := infoOf(b, mimeType)
	return &waSyncAction.StickerAction{DirectPath: &i.DirectPath, MediaKey: i.MediaKey, FileEncSHA256: i.FileEncSHA256,
		Mimetype: &i.Mimetype, FileLength: &i.FileLength, Width: proto.Uint32(512), Height: proto.Uint32(512),
		IsFavorite: proto.Bool(on), IsLottie: proto.Bool(mimeType == lottieMime)}
}

func hashes(list []stickerItem) (out []string) {
	for _, s := range list {
		out = append(out, s.Hash)
	}
	return out
}

// The migration names the stickers a chats.db already has, and asks for the app-state refetch that brings the favourites.
func TestMigrateStickers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chats.db")
	old, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	qs := append(migrations[:12:12], "PRAGMA user_version = 12", // before stickers
		`INSERT INTO chats(jid, kind) VALUES('a@lid', 0)`,
		`DELETE FROM engine_state`, // the earlier refetch has been done
		`INSERT INTO messages(chat, id, ts, out, state, kind, media) VALUES('a@lid', 'S1', 1, 0, 1, 5, '{"fileSha256":"AQID"}')`,
		`INSERT INTO messages(chat, id, ts, out, state, kind) VALUES('a@lid', 'S2', 2, 0, 1, 5)`,
		`INSERT INTO messages(chat, id, ts, out, state, kind, media) VALUES('a@lid', 'P1', 3, 0, 1, 1, '{"fileSha256":"AQID"}')`)
	for _, q := range qs {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()
	db, err := openChats(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sticker := func(id string) (s sql.NullString) {
		if err := db.QueryRow("SELECT sticker FROM messages WHERE id = ?", id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if s := sticker("S1"); s.String != "AQID" {
		t.Errorf("a sticker row: %+v, want AQID", s)
	}
	if sticker("S2").Valid || sticker("P1").Valid {
		t.Error("a sticker with no download details, or a photo, was named")
	}
	var flag, idx, faves int
	db.QueryRow("SELECT count(*) FROM engine_state WHERE key = 'appstate_resync' AND value = 1").Scan(&flag)
	db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'messages_sticker'").Scan(&idx)
	if err := db.QueryRow("SELECT count(*) FROM sticker_faves").Scan(&faves); err != nil || flag != 1 || idx != 1 {
		t.Errorf("refetch flag %d, index %d, favourites %v", flag, idx, err)
	}
}

// A received sticker's row is named by its file's hash; nothing else is.
func TestStickerRowHash(t *testing.T) {
	sha := bytes.Repeat([]byte{4}, 32)
	got := func(m *waE2E.Message) string {
		r, _ := rowFromMessage("c", &events.Message{Message: m, Info: types.MessageInfo{ID: "id"}}, "")
		return r.Sticker
	}
	if s := got(&waE2E.Message{StickerMessage: &waE2E.StickerMessage{DirectPath: proto.String("/s"), FileSHA256: sha}}); s != hashOf(4) {
		t.Errorf("a sticker: %q", s)
	}
	lottie := &waE2E.Message{LottieStickerMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
		DirectPath: proto.String("/s"), FileSHA256: sha, Mimetype: proto.String(lottieMime), IsLottie: proto.Bool(true), IsAnimated: proto.Bool(true)}}}}
	r, ok := rowFromMessage("c", &events.Message{Message: lottie, Info: types.MessageInfo{ID: "id"}}, "")
	if !ok || r.Kind != msgSticker || r.Sticker != hashOf(4) || r.MediaState != 1 || r.Extra != `{"animated":true}` || !bytes.Contains([]byte(r.Media), []byte(lottieMime)) {
		t.Errorf("a wrapped Lottie sticker: %+v", r)
	}
	for name, m := range map[string]*waE2E.Message{
		"no upload": {StickerMessage: &waE2E.StickerMessage{FileSHA256: sha}},
		"no hash":   {StickerMessage: &waE2E.StickerMessage{DirectPath: proto.String("/s")}},
		"a photo":   {ImageMessage: &waE2E.ImageMessage{DirectPath: proto.String("/s"), FileSHA256: sha}},
		"a text":    {Conversation: proto.String("hi")},
	} {
		if s := got(m); s != "" {
			t.Errorf("%s: %q, want none", name, s)
		}
	}
}

// Every insert writes the hash and every read brings it back; a forward of a sticker keeps it.
func TestStickerForwardCopy(t *testing.T) {
	st, _ := historyFixture(t)
	if err := st.runOp(func(t *storeTx) {
		t.ensureChat(stickerChat, chatUser)
		t.insertMessage(stickerRow("F1", 1, false, 1, "image/webp"))
		t.insertMessage(msgRow{Chat: stickerChat, ID: "F2", TS: 2, Kind: msgText, Text: "hi"})
	}); err != nil {
		t.Fatal(err)
	}
	if r, ok := st.row(stickerChat, "F1"); !ok || r.Sticker != hashOf(1) {
		t.Errorf("the row reads back as %q", r.Sticker)
	}
	if n := rows(st, "SELECT count(*) FROM messages WHERE sticker IS NOT NULL"); n != 1 {
		t.Errorf("%d rows are named, want 1: a text has no sticker", n)
	}
	msg, row, err := st.forwardCopy(forwardReq{Chat: stickerChat, ID: "F1", To: "b@lid"}, "NEW", 9)
	if err != nil || row.Sticker != hashOf(1) || row.Kind != msgSticker || msg.GetStickerMessage().GetDirectPath() != "/v/s1" {
		t.Errorf("a forwarded sticker: %q %v %v", row.Sticker, msg, err)
	}
}

// A favourite from app state is stored whole, a repeat only refreshes it, and the cards of the messages that carry the
// sticker are told when it appears or goes (their fave flag), and no others.
func TestFaveEvent(t *testing.T) {
	st, cli := historyFixture(t)
	tx, _ := newTestTx(t)
	tx.ensureChat(stickerChat, chatUser)
	for _, r := range []msgRow{stickerRow("M1", 1, false, 1, "image/webp"), stickerRow("M2", 2, true, 1, "image/webp"),
		stickerRow("M3", 3, false, 2, "image/webp"), {Chat: stickerChat, ID: "M4", TS: 4, Kind: msgText, Text: "hi"}} {
		tx.insertMessage(r)
	}
	send := func(hash string, a *waSyncAction.StickerAction, secs int64) {
		tx.msgs = map[string]map[string]bool{}
		st.dispatch(tx, cli, &events.FavoriteSticker{FileSHA256: hash, Timestamp: time.Unix(secs, 0), Action: a})
	}
	told := func() string {
		var ids []string
		for id := range tx.msgs[stickerChat] {
			ids = append(ids, id)
		}
		return fmt.Sprint(len(ids), tx.msgs[stickerChat]["M1"], tx.msgs[stickerChat]["M2"])
	}
	faves := func() int { return count(t, tx, "SELECT count(*) FROM sticker_faves") }

	send(hashOf(1), faveOf(1, "image/webp", true), 100)
	var media string
	var w, ts int
	tx.tx.QueryRow("SELECT media, width, ts FROM sticker_faves WHERE hash = ?", hashOf(1)).Scan(&media, &w, &ts)
	var info mediaInfo
	json.Unmarshal([]byte(media), &info)
	if want := infoOf(1, "image/webp"); !bytes.Equal(info.FileSHA256, want.FileSHA256) || info.DirectPath != want.DirectPath ||
		!bytes.Equal(info.MediaKey, want.MediaKey) || !bytes.Equal(info.FileEncSHA256, want.FileEncSHA256) ||
		info.Mimetype != want.Mimetype || info.FileLength != 900 || w != 512 || ts != 100 {
		t.Errorf("stored %+v width %d ts %d", info, w, ts)
	}
	if got := told(); got != "2 true true" {
		t.Errorf("a new favourite told %s: want M1 and M2 only", got)
	}
	send(hashOf(1), faveOf(1, "image/webp", true), 200)
	tx.tx.QueryRow("SELECT ts FROM sticker_faves WHERE hash = ?", hashOf(1)).Scan(&ts)
	if faves() != 1 || ts != 200 || len(tx.msgs) != 0 {
		t.Errorf("the same favourite again: %d rows, ts %d, told %v", faves(), ts, tx.msgs)
	}
	send(hashOf(1), faveOf(1, "image/webp", false), 300)
	if faves() != 0 || told() != "2 true true" {
		t.Errorf("an unfavourite: %d rows, told %s", faves(), told())
	}
	send(hashOf(1), faveOf(1, "image/webp", false), 400)
	if len(tx.msgs) != 0 {
		t.Errorf("an unfavourite of what isn't one told %v", tx.msgs)
	}
	// Lottie stickers are kept like any other; a hash that isn't base64 is dropped.
	send(hashOf(5), faveOf(5, lottieMime, true), 500)
	send("not base64!", faveOf(6, "image/webp", true), 500)
	if faves() != 1 || count(t, tx, "SELECT count(*) FROM sticker_faves WHERE hash = ? AND media LIKE '%application/was%'", hashOf(5)) != 1 {
		t.Errorf("%d favourites: want the Lottie one only", faves())
	}
}

// The lists: favourites newest first; recents are what we sent, once each, newest first, 32 at most, deleted and
// received ones left out; a path is a file that is here, from a message or from stickers.file's copy.
func TestStickerLists(t *testing.T) {
	tx, _ := newTestTx(t)
	dir := t.TempDir()
	tx.ensureChat(stickerChat, chatUser)
	here := filepath.Join(dir, "media", "B1.webp")
	if err := writeFile(here, []byte("webp")); err != nil {
		t.Fatal(err)
	}
	b1 := stickerRow("B1", 20, true, 2, "image/webp")
	b1.MediaState, b1.Path = 2, here
	d1 := stickerRow("D1", 50, true, 4, "image/webp")
	d1.Deleted = true
	for _, r := range []msgRow{stickerRow("A1", 10, true, 1, "image/webp"), b1, stickerRow("A2", 30, true, 1, "image/webp"),
		stickerRow("C1", 40, false, 3, "image/webp"), d1, stickerRow("E1", 5, true, 5, lottieMime),
		{Chat: stickerChat, ID: "T1", TS: 60, Out: true, Kind: msgText, Text: "hi"}} {
		tx.insertMessage(r)
	}
	copyOf := stickerCopy(dir, hashOf(1), false)
	if err := writeFile(copyOf, []byte("webp")); err != nil {
		t.Fatal(err)
	}
	tx.setFave(hashOf(1), faveOf(1, "image/webp", true), 100)
	tx.setFave(hashOf(6), faveOf(6, lottieMime, true), 200)

	faves, recent := tx.stickerLists(dir)
	if got, want := fmt.Sprint(hashes(faves)), fmt.Sprint([]string{hashOf(6), hashOf(1)}); got != want {
		t.Errorf("favourites %s, want %s", got, want)
	}
	if !faves[0].Lottie || faves[1].Lottie || faves[0].W != 512 || faves[0].Path != "" || faves[1].Path != copyOf {
		t.Errorf("favourites %+v", faves)
	}
	if got, want := fmt.Sprint(hashes(recent)), fmt.Sprint([]string{hashOf(1), hashOf(2), hashOf(5)}); got != want {
		t.Errorf("recent %s, want %s: ours, once each, newest first, no deleted", got, want)
	}
	if recent[0].Path != copyOf || recent[1].Path != here || recent[2].Path != "" || !recent[2].Lottie || recent[1].H != 512 {
		t.Errorf("recent %+v", recent)
	}
	if j, _ := json.Marshal(recent[1]); string(j) != fmt.Sprintf(`{"hash":%q,"w":512,"h":512,"path":%q}`, hashOf(2), here) {
		t.Errorf("a sticker reads %s", j)
	}
	for i := 0; i < 40; i++ {
		tx.insertMessage(stickerRow(fmt.Sprint("N", i), int64(1000+i), true, byte(100+i), "image/webp"))
	}
	if _, recent = tx.stickerLists(dir); len(recent) != 32 || recent[0].Hash != hashOf(139) {
		t.Errorf("%d recent, the newest %v: want 32, from the newest", len(recent), recent[0])
	}
}

// What sticker.send puts on the wire and in the chat: from a favourite, from a row we have a file for, and from a
// Lottie sticker.
func TestStickerSend(t *testing.T) {
	tx, _ := newTestTx(t)
	dir := t.TempDir()
	tx.ensureChat(stickerChat, chatUser)

	tx.setFave(hashOf(7), faveOf(7, "image/webp", true), 1)
	src, ok := tx.stickerSource(dir, hashOf(7))
	if !ok {
		t.Fatal("a favourite is a source")
	}
	msg, row := stickerOut(src, hashOf(7), stickerChat, "NEW1", 50)
	d, i := msg.GetStickerMessage(), infoOf(7, "image/webp")
	if msg.LottieStickerMessage != nil || flatten(msg).GetStickerMessage() != d {
		t.Error("a WebP sticker goes as it is")
	}
	if d.GetDirectPath() != i.DirectPath || !bytes.Equal(d.GetMediaKey(), i.MediaKey) || !bytes.Equal(d.GetFileEncSHA256(), i.FileEncSHA256) ||
		!bytes.Equal(d.GetFileSHA256(), i.FileSHA256) || d.GetFileLength() != 900 || d.GetMimetype() != "image/webp" ||
		d.GetWidth() != 512 || d.GetHeight() != 512 || d.GetIsAnimated() || d.GetIsLottie() || d.PngThumbnail != nil || d.URL != nil {
		t.Errorf("message from a favourite: %v", d)
	}
	var media mediaInfo
	json.Unmarshal([]byte(row.Media), &media)
	if row.Kind != msgSticker || !row.Out || row.State != stateSending || row.Sticker != hashOf(7) || row.MediaState != 1 || row.Path != "" ||
		row.Size != 900 || row.Width != 512 || row.Extra != "" || row.QuoteID != "" || row.TS != 50 || row.Chat != stickerChat || row.ID != "NEW1" ||
		media.DirectPath != i.DirectPath || !bytes.Equal(media.FileSHA256, i.FileSHA256) {
		t.Errorf("row from a favourite: %+v", row)
	}
	if !tx.insertMessage(row) {
		t.Fatal("the row doesn't go in")
	}
	if n := count(t, tx, "SELECT count(*) FROM messages WHERE id = 'NEW1' AND sticker = ?", hashOf(7)); n != 1 {
		t.Error("the queued row is not named")
	}

	// From a message of ours: its thumbnail, its animation mark, and the file it has.
	file := filepath.Join(dir, "media", "R1.webp")
	if err := writeFile(file, []byte("webp")); err != nil {
		t.Fatal(err)
	}
	r := stickerRow("R1", 5, true, 8, "image/webp")
	r.Thumb, r.Extra, r.MediaState, r.Path = []byte{9}, `{"animated":true}`, 2, file
	tx.insertMessage(r)
	src, _ = tx.stickerSource(dir, hashOf(8))
	msg, row = stickerOut(src, hashOf(8), stickerChat, "NEW2", 51)
	if d = msg.GetStickerMessage(); !d.GetIsAnimated() || !bytes.Equal(d.PngThumbnail, []byte{9}) || d.GetWidth() != 512 ||
		row.MediaState != 2 || row.Path != file || row.Extra != `{"animated":true}` || !bytes.Equal(row.Thumb, []byte{9}) {
		t.Errorf("from a row: %v %+v", d, row)
	}

	// No mark in the row: the file says. A static one doesn't.
	anim := append([]byte("RIFF\x20\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00"), 0x12)
	for hash, c := range map[byte]struct {
		head []byte
		want bool
	}{9: {anim, true}, 10: {[]byte("RIFF\x20\x00\x00\x00WEBPVP8 \x0a\x00\x00\x00\x00"), false}} {
		p := filepath.Join(dir, "media", fmt.Sprint("X", hash, ".webp"))
		writeFile(p, c.head)
		x := stickerRow(fmt.Sprint("X", hash), 6, true, hash, "image/webp")
		x.MediaState, x.Path = 2, p
		tx.insertMessage(x)
		if src, _ = tx.stickerSource(dir, hashOf(hash)); src.animated != c.want {
			t.Errorf("a file that is animated %v read as %v", c.want, src.animated)
		}
	}

	// A Lottie sticker is animated and says so; its favourite's action does too.
	tx.setFave(hashOf(11), faveOf(11, lottieMime, true), 2)
	src, _ = tx.stickerSource(dir, hashOf(11))
	msg, row = stickerOut(src, hashOf(11), stickerChat, "NEW3", 52)
	d = msg.GetLottieStickerMessage().GetMessage().GetStickerMessage()
	if msg.StickerMessage != nil || d == nil || !d.GetIsLottie() || !d.GetIsAnimated() || row.Extra != `{"animated":true}` || d.GetMimetype() != lottieMime {
		t.Errorf("a Lottie sticker goes wrapped: %v %+v", msg, row)
	}
	if flatten(msg).GetStickerMessage() != d { // where sticker.send puts the quote
		t.Error("the wrapped sticker isn't the one flatten finds")
	}

	// Not here, or only as a deleted message: no source.
	del := stickerRow("G1", 7, true, 12, "image/webp")
	del.Deleted = true
	tx.insertMessage(del)
	for _, h := range []string{hashOf(99), hashOf(12)} {
		if _, ok := tx.stickerSource(dir, h); ok {
			t.Errorf("%s is a source", h)
		}
	}
}

// The favourite's action: the media details, the direct path as a URL, the Lottie mark; and hypermeow takes it as a patch.
func TestStickerAction(t *testing.T) {
	for _, mimeType := range []string{"image/webp", lottieMime} {
		src := stickerSrc{info: infoOf(3, mimeType), w: 512, h: 256}
		for _, on := range []bool{true, false} {
			a := stickerAction(src, on)
			if a.GetURL() != "https://mmg.whatsapp.net/v/s3" || a.GetDirectPath() != "/v/s3" || a.GetIsFavorite() != on || a.GetMimetype() != mimeType ||
				a.GetIsLottie() != (mimeType == lottieMime) || a.GetWidth() != 512 || a.GetHeight() != 256 || a.GetFileLength() != 900 ||
				!bytes.Equal(a.GetMediaKey(), src.info.MediaKey) || !bytes.Equal(a.GetFileEncSHA256(), src.info.FileEncSHA256) {
				t.Errorf("%s, on %v: %v", mimeType, on, a)
			}
			p, err := appstate.BuildFavoriteSticker(bytes.Repeat([]byte{3}, 32), a)
			if err != nil || p.Mutations[0].Index[1] != hashOf(3) {
				t.Errorf("%s, on %v: patch %+v, %v", mimeType, on, p, err)
			}
		}
	}
}

func TestAnimatedWebP(t *testing.T) {
	head := func(chunk string, flags byte) []byte {
		return append([]byte("RIFF\x20\x00\x00\x00WEBP"+chunk+"\x0a\x00\x00\x00"), flags)
	}
	for name, c := range map[string]struct {
		b    []byte
		want bool
	}{
		"animated":            {head("VP8X", 0x02), true},
		"animated with alpha": {head("VP8X", 0x12), true},
		"alpha only":          {head("VP8X", 0x10), false},
		"lossy, no extension": {head("VP8 ", 0x02), false},
		"not WebP":            {append([]byte("PK\x03\x04"), make([]byte, 30)...), false},
		"too short":           {head("VP8X", 0x02)[:20], false},
		"empty":               {nil, false},
	} {
		if got := isAnimatedWebP(c.b); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
	p := filepath.Join(t.TempDir(), "a.webp")
	os.WriteFile(p, append(head("VP8X", 0x02), make([]byte, 100)...), 0o644)
	if !fileAnimated(p) || fileAnimated(p+".gone") {
		t.Error("a file is read from its head; a missing one is not animated")
	}
}

// stickerCopy names the file by the hash's hex: .webp, or .was for Lottie.
func TestStickerCopy(t *testing.T) {
	h := hex.EncodeToString(bytes.Repeat([]byte{0xab}, 32))
	if got := stickerCopy("/d", hashOf(0xab), false); got != "/d/stickers/"+h+".webp" {
		t.Errorf("webp: %s", got)
	}
	if got := stickerCopy("/d", hashOf(0xab), true); got != "/d/stickers/"+h+".was" {
		t.Errorf("lottie: %s", got)
	}
}

// The phone's app state has favourites that carry the hash alone (the user's Q10, 2026-11-22): such a one is left out of
// the lists and can't be sent, until a message brings the sticker's details; a row we sent with no upload never stands in.
func TestBareFave(t *testing.T) {
	tx, _ := newTestTx(t)
	dir := t.TempDir()
	tx.ensureChat(stickerChat, chatUser)
	bare := &waSyncAction.StickerAction{Mimetype: proto.String("image/webp"), Width: proto.Uint32(200), Height: proto.Uint32(200),
		IsFavorite: proto.Bool(true)}
	tx.setFave(hashOf(9), bare, 100)
	sent := stickerRow("S1", 10, true, 9, "image/webp")
	sent.Media = `{"directPath":"","mediaKey":"","fileSha256":"` + hashOf(9) + `","mimetype":"image/webp"}`
	tx.insertMessage(sent)
	if faves, recent := tx.stickerLists(dir); len(faves) != 0 || len(recent) != 0 {
		t.Errorf("favourites %+v, recent %+v: want none, nothing to fetch them with", faves, recent)
	}
	if src, ok := tx.stickerSource(dir, hashOf(9)); !ok || src.usable() {
		t.Errorf("source %+v %v: want found, not usable", src, ok)
	}
	tx.insertMessage(stickerRow("R1", 20, false, 9, "image/webp"))
	faves, _ := tx.stickerLists(dir)
	src, _ := tx.stickerSource(dir, hashOf(9))
	if len(faves) != 1 || !src.usable() || src.info.DirectPath != "/v/s9" || src.w != 512 {
		t.Errorf("favourites %+v, source %+v: want the received message's details", faves, src)
	}
}

// An upload's expiry is its direct path's oe (hex unix time); the CDN answers 403 after it (the user's Q10, 2026-11-22).
func TestUploadExpired(t *testing.T) {
	now := time.Unix(0x6AEF0C3F, 0)
	for path, want := range map[string]bool{
		"/v/t62.15575-24/1_n.enc?ccb=11-4&oh=x&oe=6AEF0C3F&_nc_sid=5e03e0":               true,  // at it
		"/v/t62.15575-24/1_n.enc?ccb=11-4&oh=x&oe=" + fmt.Sprintf("%X", 0x6AEF0C3F+1800): true,  // within the hour's margin
		"/v/t62.15575-24/1_n.enc?ccb=11-4&oh=x&oe=" + fmt.Sprintf("%X", 0x6AEF0C3F+7200): false, // two hours left
		"/v/s9": false, // no oe: no expiry known
	} {
		if got := uploadExpired(path, now); got != want {
			t.Errorf("%s: expired %v, want %v", path, got, want)
		}
	}
	tx, _ := newTestTx(t)
	dir := t.TempDir()
	tx.ensureChat(stickerChat, chatUser)
	old := infoOf(12, "image/webp")
	old.DirectPath = "/v/x.enc?oe=6AEF0C3F"
	media, _ := json.Marshal(old)
	r := stickerRow("X1", 10, true, 12, "image/webp")
	r.Media = string(media)
	tx.insertMessage(r)
	if _, recent := tx.stickerLists(dir); len(recent) != 0 {
		t.Errorf("recent %+v: an expired upload with no file here is left out", recent)
	}
	here := stickerCopy(dir, hashOf(12), false)
	if err := writeFile(here, []byte("webp")); err != nil {
		t.Fatal(err)
	}
	if _, recent := tx.stickerLists(dir); len(recent) != 1 || recent[0].Path != here {
		t.Errorf("recent %+v: with the file here it is listed, and sent by uploading it again", recent)
	}
}
