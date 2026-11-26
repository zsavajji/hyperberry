package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
)

// Every send of ours is made from its row in chats.db alone (the pipeline, outgoing.go): the first try, the one after a
// restart, and the one msg.retry asks for. It goes with the row's id, so WhatsApp drops a copy of one that had gone after all.

// buildAttempt is the attempt that sends our row. An error says it can't be rebuilt: its file is gone, it is a kind we don't
// send, a sticker has no source; the caller fails the row.
// ponytail: a poll goes with a new secret.
func (s *Session) buildAttempt(row msgRow) (attempt, error) {
	jid, err := types.ParseJID(row.Chat)
	if err != nil || jid.IsEmpty() {
		return nil, errors.New("not a chat key: " + row.Chat)
	}
	chat, id := row.Chat, row.ID
	if forwardScore(row.Extra) == 0 { // a forward is one message, whatever it holds: its upload is on the server already
		switch row.Kind {
		case msgPhoto, msgVideo, msgAudio, msgDoc:
			if fi, err := os.Stat(row.Path); err != nil || !fi.Mode().IsRegular() {
				return nil, errors.New("its file is gone: " + row.Path)
			}
			o := s.mediaOf(row)
			return func(c *whatsmeow.Client) (sendDone, error) { return s.uploadMedia(c, jid, o) }, nil
		case msgVoice:
			ogg, err := os.ReadFile(oggPath(s.srv.dir, id))
			if err != nil {
				return nil, errors.New("its recording is gone")
			}
			n := &voiceNote{ogg: ogg, wave: row.Waveform, secs: row.Duration, quote: s.quoteOf(row)}
			return func(c *whatsmeow.Client) (sendDone, error) { return s.uploadVoice(c, jid, chat, id, n) }, nil
		case msgSticker:
			dir, err := filepath.Abs(s.srv.dir)
			if err != nil {
				return nil, err
			}
			var src stickerSrc
			var ok bool
			if err := s.store.runOp(func(t *storeTx) { src, ok = t.stickerSource(dir, row.Sticker) }); err != nil {
				return nil, err
			}
			if !ok || (!src.usable() && src.path == "") {
				return nil, errors.New("its sticker has no file to send")
			}
			q := s.quoteOf(row)
			return func(c *whatsmeow.Client) (sendDone, error) {
				return s.sendSticker(c, jid, chat, id, row.Sticker, &src, q)
			}, nil
		}
	}
	msg, err := s.resendMessage(s.client(), row)
	if err != nil {
		return nil, err
	}
	return func(c *whatsmeow.Client) (sendDone, error) { return s.sendMsg(c, jid, id, msg) }, nil
}

// mediaOf is the upload of our media row, rebuilt: the mimetype by its file's extension, and a video's poster read back
// from the file the send kept (none: it goes without).
func (s *Session) mediaOf(row msgRow) *mediaOut {
	_, mimeType := sendKind(row.Path)
	poster, _ := os.ReadFile(posterPath(s.srv.dir, row.ID))
	return &mediaOut{row: row, mime: mimeType, quote: s.quoteOf(row), poster: poster}
}

// resendMessage is the wire message of our row that goes as it is, with nothing to upload: a text (a reply, with
// mentions), a reply to a story, a poll, a forward of any kind.
func (s *Session) resendMessage(cli *whatsmeow.Client, row msgRow) (*waE2E.Message, error) {
	var x struct {
		Mentions  []mention
		Forwarded int
		Story     bool
		Options   []string
		Max       int
	}
	json.Unmarshal([]byte(row.Extra), &x) // "" or not ours: none of these
	switch {
	case x.Forwarded > 0:
		row.Extra = withKey(row.Extra, "forwarded", strconv.Itoa(x.Forwarded-1)) // the row has the score forwardContent adds
		msg, _, err := forwardContent(row)
		return msg, err
	case row.Kind == msgPoll:
		if len(x.Options) < minPollOptions {
			return nil, errors.New("its poll has no options")
		}
		return cli.BuildPollCreation(row.Text, x.Options, x.Max), nil
	case row.Kind != msgText:
		return nil, fmt.Errorf("a message of kind %d can't be sent again", row.Kind)
	case x.Story: // the story itself if it is still here, else the one line the row keeps of it
		quoted := quotedStory(msgText, row.QuotePreview, nil)
		if kind, text, thumb, ok := s.store.storyQuote(row.Chat, row.QuoteID); ok {
			quoted = quotedStory(kind, text, thumb)
		}
		return storyReplyMessage(row.Chat, row.QuoteID, row.Text, quoted), nil
	}
	return textMessage(cli, row.Chat, row.Text, s.quoteOf(row), x.Mentions), nil
}

// quoteOf is the message our row quotes, read again; empty for none, or gone since: it is sent without the quote.
func (s *Session) quoteOf(row msgRow) msgRow {
	if q, ok := s.store.row(row.Chat, row.QuoteID); ok {
		return q
	}
	return msgRow{}
}
