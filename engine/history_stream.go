package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// History sync, a chat at a time. The phone caps the engine at about 30 MB, and hypermeow's own
// way is to acknowledge a chunk before it has downloaded it, then hold the blob, its inflated
// copy and the decoded whole in memory at once: a big chunk would kill the engine and, being
// acknowledged, never come again. So Store.handle gives the notifications to historyQueue
// instead (hypermeow only dispatches them: session.go turns its own handling off), and a chunk
// goes like this, one at a time, in the order it came:
//
//	data/history/<id>.notif  the notification, written first: a crash can resume from it
//	data/history/<id>.z      the blob, streamed to disk
//	pass 1                   everything but the chats, which hypermeow stores for its side effects
//	                         (LID mappings, contacts, push names, ...); it has to come first, so
//	                         the chats' keys resolve LIDs
//	pass 2                   the chats, each alone through hypermeow and then the writer
//	the end                  the chunk's HistorySync event, once its chats are in chats.db, then
//	                         the receipt to the phone, then the files go
//
// A crash leaves the files; the next Connected queues the chunk again, once (<id>.retry).

// The fields of waHistorySync.HistorySync that the stream tells apart.
const (
	hsSyncType      protowire.Number = 1
	hsConversations protowire.Number = 2
	hsChunkOrder    protowire.Number = 5
	hsProgress      protowire.Number = 6
)

// histFieldMax is the longest field the stream will hold: a chat longer than this couldn't be
// decoded within the engine's memory anyway, so it is skipped (and logged) instead of killing
// the engine. A var for the test.
// ponytail: one chat is decoded whole, so a huge group's 90 days can be several MB at once; if
// that bites, stream the chat's own messages field the same way (scanHistory over its bytes).
var histFieldMax = 16 << 20

// scanHistory walks the top-level fields of the zlib-compressed HistorySync in the file at path
// as a stream: the blob is never whole in memory. A field keep refuses is discarded unread. each
// gets a kept field's number and its raw bytes, tag and length included, as they are in the blob;
// raw is reused once each returns, and is nil for a kept field longer than histFieldMax.
func scanHistory(path string, keep func(protowire.Number) bool, each func(num protowire.Number, raw []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := zlib.NewReader(bufio.NewReader(f))
	if err != nil {
		return err
	}
	defer zr.Close()
	br := bufio.NewReaderSize(zr, 32<<10)
	var raw []byte
	for {
		raw = raw[:0]
		tag, err := readVarint(br, &raw)
		if err == io.EOF {
			return nil // between two fields, so zlib's checksum has held too
		}
		if err != nil {
			return err
		}
		num, typ := protowire.DecodeTag(tag)
		if num < 1 {
			return errors.New("history: bad field number")
		}
		wanted := keep(num)
		switch typ {
		case protowire.VarintType:
			_, err = readVarint(br, &raw)
		case protowire.Fixed32Type:
			err = readRaw(br, &raw, 4)
		case protowire.Fixed64Type:
			err = readRaw(br, &raw, 8)
		case protowire.BytesType:
			var n uint64
			if n, err = readVarint(br, &raw); err != nil {
				break
			}
			if n > math.MaxInt32 {
				return errors.New("history: field too long")
			}
			if wanted && n <= uint64(histFieldMax) {
				err = readRaw(br, &raw, int(n))
			} else {
				_, err = io.CopyN(io.Discard, br, int64(n))
				if wanted {
					raw = nil
				}
			}
		default:
			return fmt.Errorf("history: wire type %d", typ)
		}
		if err == io.EOF {
			err = io.ErrUnexpectedEOF // inside a field
		}
		if err != nil {
			return err
		}
		if wanted {
			if err := each(num, raw); err != nil {
				return err
			}
		}
	}
}

// readVarint reads a varint from r, appending its bytes to raw.
func readVarint(r io.ByteReader, raw *[]byte) (uint64, error) {
	start := len(*raw)
	for range 10 { // a varint is at most 10 bytes
		b, err := r.ReadByte()
		if err != nil {
			if err == io.EOF && len(*raw) > start {
				err = io.ErrUnexpectedEOF
			}
			return 0, err
		}
		*raw = append(*raw, b)
		if b < 0x80 {
			v, n := protowire.ConsumeVarint((*raw)[start:])
			return v, protowire.ParseError(n)
		}
	}
	return 0, errors.New("history: varint too long")
}

// readRaw reads n bytes from r, appending them to raw.
func readRaw(r io.Reader, raw *[]byte, n int) error {
	l := len(*raw)
	*raw = slices.Grow(*raw, n)[:l+n]
	_, err := io.ReadFull(r, (*raw)[l:])
	return err
}

// zlibBytes compresses parts, joined, the way a history blob comes: hypermeow's parser takes
// nothing else. The fastest level, since the CPU on the phone is dear and the bytes never travel.
func zlibBytes(parts ...[]byte) []byte {
	var b bytes.Buffer
	w, _ := zlib.NewWriterLevel(&b, zlib.BestSpeed)
	for _, p := range parts {
		w.Write(p)
	}
	w.Close()
	return b.Bytes()
}

type histChunk struct {
	cli   *whatsmeow.Client
	id    string
	notif *waE2E.HistorySyncNotification
}

// historyQueue works the chunks of Store.hist: one worker, so one chunk at a time.
type historyQueue struct {
	st      *Store
	dir     string
	wake    chan struct{}
	mu      sync.Mutex
	todo    []histChunk
	pending map[string]bool // ids queued or being worked
}

func newHistoryQueue(st *Store) *historyQueue {
	q := &historyQueue{st: st, dir: filepath.Join(st.srv.dir, "history"), wake: make(chan struct{}, 1), pending: map[string]bool{}}
	go q.run()
	return q
}

func (q *historyQueue) path(id, ext string) string {
	return filepath.Join(q.dir, fileSafe.Replace(id)+ext)
}

// remove takes everything of a chunk off the disk.
func (q *historyQueue) remove(id string) {
	for _, ext := range []string{".notif", ".z", ".z.tmp", ".retry"} {
		os.Remove(q.path(id, ext))
	}
}

// takeHistory is handle's first look at an event: a history-sync notification doesn't go to the
// writer but to the chunk queue. It reports whether it took the event.
func (st *Store) takeHistory(cli *whatsmeow.Client, evt any) bool {
	e, ok := evt.(*events.Message)
	if !ok || !e.Info.IsFromMe {
		return false
	}
	n := e.Message.GetProtocolMessage().GetHistorySyncNotification()
	if n == nil {
		return false
	}
	st.hist.add(cli, e.Info.ID, n, false)
	return true
}

// add queues a chunk, unless it is queued or being worked already. A live chunk is written to
// disk first. A resumed one (found on disk after a crash or a failure) gets its .retry marker,
// or, with the marker there already, it has failed twice and is dropped.
func (q *historyQueue) add(cli *whatsmeow.Client, id string, notif *waE2E.HistorySyncNotification, resumed bool) {
	if cli == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.pending[id] {
		return
	}
	if resumed {
		if _, err := os.Stat(q.path(id, ".retry")); err == nil {
			q.remove(id)
			q.st.log.Warnf("history chunk %s dropped after a retry", id)
			return
		}
		if err := os.WriteFile(q.path(id, ".retry"), nil, 0o600); err != nil {
			q.st.log.Warnf("history chunk %s: %v", id, err)
		}
	} else if b, err := proto.Marshal(notif); err == nil {
		os.MkdirAll(q.dir, 0o700)
		if err := os.WriteFile(q.path(id, ".notif"), b, 0o600); err != nil {
			q.st.log.Warnf("history chunk %s: not kept for a retry: %v", id, err)
		}
	}
	q.pending[id] = true
	q.todo = append(q.todo, histChunk{cli, id, notif})
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// resume queues the chunks left on disk. It runs at every Connected, and the chunks queued or
// being worked are skipped, so a reconnect doesn't count as a retry.
func (q *historyQueue) resume(cli *whatsmeow.Client) {
	ents, _ := os.ReadDir(q.dir)
	for _, ent := range ents {
		id, ok := strings.CutSuffix(ent.Name(), ".notif")
		if !ok {
			continue
		}
		notif := new(waE2E.HistorySyncNotification)
		b, err := os.ReadFile(q.path(id, ".notif"))
		if err == nil {
			err = proto.Unmarshal(b, notif)
		}
		if err != nil {
			q.st.log.Warnf("history chunk %s dropped: %v", id, err)
			q.remove(id)
			continue
		}
		q.add(cli, id, notif, true)
	}
}

func (q *historyQueue) run() {
	for {
		select {
		case <-q.wake:
		case <-q.st.done:
			return
		}
		for {
			q.mu.Lock()
			if len(q.todo) == 0 {
				q.mu.Unlock()
				break
			}
			c := q.todo[0]
			q.todo = q.todo[1:]
			q.mu.Unlock()
			if err := q.process(c); err != nil {
				q.st.log.Warnf("history chunk %s kept for a retry: %v", c.id, err)
			}
			q.mu.Lock()
			delete(q.pending, c.id)
			q.mu.Unlock()
		}
	}
}

// runOp has the writer run f and waits for it, so what the worker reads next doesn't pile up in
// st.ops behind it.
func (st *Store) runOp(f func(*storeTx)) error {
	done := make(chan struct{})
	select {
	case st.ops <- func(t *storeTx) { defer close(done); f(t) }:
	case <-st.done:
		return errors.New("store closed")
	}
	select {
	case <-done:
		return nil
	case <-st.done:
		return errors.New("store closed")
	}
}

// fetch puts the chunk's blob in the file blob, whole or not at all.
func (q *historyQueue) fetch(c histChunk, blob string) error {
	ctx, cancel := context.WithTimeout(context.Background(), mediaTimeout)
	defer cancel()
	_, err := downloadTo(blob+".tmp", func(f *os.File) (string, error) {
		if p := c.notif.InitialHistBootstrapInlinePayload; p != nil {
			_, err := f.Write(p)
			return "", err
		}
		return "", c.cli.DownloadToFile(ctx, c.notif, f)
	}, func(string) string { return blob })
	return err
}

// process works one chunk. An error leaves its files, and no receipt goes: it is for a retry.
// ponytail: an Unlink while a chunk is worked can still write its chats into the new, empty
// chats.db (hypermeow's own path had the same gap); a generation counter bumped in Store.clear
// would stop it.
func (q *historyQueue) process(c histChunk) error {
	st, cli, id := q.st, c.cli, c.id
	ctx, t0, blob := context.Background(), time.Now(), q.path(id, ".z")

	// A blob left by a crash is whole: fetch moves it in place only when it is.
	fi, err := os.Stat(blob)
	if err != nil {
		if err = q.fetch(c, blob); err == nil {
			fi, err = os.Stat(blob)
		}
	}
	if err != nil {
		return fmt.Errorf("blob: %w", err)
	}
	st.log.Infof("history chunk %s: %d bytes compressed, %v to get, %s", id, fi.Size(), time.Since(t0).Round(time.Millisecond), memUsage())

	// Pass 1: all but the chats. header (the sync type, the chunk order, the progress) goes with
	// each chat later, so hypermeow sees a chunk of one chat.
	// ponytail: the rest (status messages, push names, contacts, ...) is held and decoded whole;
	// it is small beside the chats. If an account's isn't, scan its big fields apart too.
	var rest, header []byte
	err = scanHistory(blob, func(n protowire.Number) bool { return n != hsConversations }, func(n protowire.Number, raw []byte) error {
		rest = append(rest, raw...)
		if n == hsSyncType || n == hsChunkOrder || n == hsProgress {
			header = append(header, raw...)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("pass 1: %w", err)
	}
	// Downloads nothing (the payload is inline), and with synchronousStorage it has stored the
	// LID mappings, contacts, push names and the rest before it returns.
	hs0, err := cli.DownloadHistorySync(ctx, &waE2E.HistorySyncNotification{InitialHistBootstrapInlinePayload: zlibBytes(rest)}, true)
	rest = nil
	if err != nil {
		return fmt.Errorf("pass 1: %w", err)
	}
	st.log.Infof("history chunk %s: all but the chats read, %s", id, memUsage())

	// Pass 2: the chats, one at a time. hypermeow stores a chat's message secrets and parses it;
	// the writer gets it as one op, and the next chat is read only once that op has run.
	chats, skipped := 0, 0
	err = scanHistory(blob, func(n protowire.Number) bool { return n == hsConversations }, func(_ protowire.Number, raw []byte) error {
		if raw == nil {
			skipped++
			st.log.Warnf("history chunk %s: a chat over %d bytes skipped", id, histFieldMax)
			return nil
		}
		hs, err := cli.DownloadHistorySync(ctx, &waE2E.HistorySyncNotification{InitialHistBootstrapInlinePayload: zlibBytes(header, raw)}, true)
		if err != nil {
			skipped++
			st.log.Warnf("history chunk %s: a chat skipped: %v", id, err)
			return nil
		}
		// Not through handle: it would fire historyIn for every chat.
		if err := st.runOp(func(t *storeTx) { st.dispatch(t, cli, &events.HistorySync{Data: hs}) }); err != nil {
			return err
		}
		if chats++; chats%50 == 0 {
			st.log.Infof("history chunk %s: %d chats, %s", id, chats, memUsage())
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("pass 2 (%d chats in): %w", chats, err)
	}

	// The end: hs0 has no chats, so it writes none, and handle fires historyIn after its commit,
	// which now means the chunk's chats are all in. The receipt waits for that commit.
	st.handle(cli, &events.HistorySync{Data: hs0, Notification: c.notif, MessageID: id})
	committed := make(chan struct{})
	st.afterCommit(func() { close(committed) })
	select {
	case <-committed:
	case <-time.After(time.Minute):
		return errors.New("chats.db did not commit")
	case <-st.done:
		return errors.New("store closed")
	}
	rctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := cli.SendProtocolMessageReceipt(rctx, id, types.ReceiptTypeHistorySync); err != nil {
		st.log.Warnf("history chunk %s: receipt: %v", id, err)
	}
	if c.notif.InitialHistBootstrapInlinePayload == nil { // as hypermeow does: the server needn't keep it
		if err := cli.DeleteMedia(rctx, whatsmeow.MediaHistory, c.notif.GetDirectPath(), c.notif.GetFileEncSHA256(), c.notif.GetEncHandle()); err != nil {
			st.log.Warnf("history chunk %s: media delete: %v", id, err)
		}
	}
	q.remove(id)
	st.log.Infof("history chunk %s: %s, chunk %d, progress %d, %d chats (%d skipped), %d bytes compressed, %v, %s",
		id, hs0.GetSyncType(), hs0.GetChunkOrder(), hs0.GetProgress(), chats, skipped, fi.Size(),
		time.Since(t0).Round(time.Millisecond), memUsage())
	return nil
}
