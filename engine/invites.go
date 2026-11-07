package main

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
)

// inviteLink is a WhatsApp group or channel link, with or without its scheme: what a tap on a link in a message hands over.
var inviteLink = regexp.MustCompile(`^(?:https?://)?(?:www\.)?(?:chat\.whatsapp\.com/(?:invite/)?([A-Za-z0-9_-]+)|whatsapp\.com/channel/([A-Za-z0-9_-]+))`)

const (
	inviteGroup   = "group"
	inviteChannel = "channel"
)

func parseInvite(url string) (kind, code string, ok bool) {
	m := inviteLink.FindStringSubmatch(strings.TrimSpace(url))
	switch {
	case m == nil:
		return "", "", false
	case m[1] != "":
		return inviteGroup, m[1], true
	}
	return inviteChannel, m[2], true
}

// ready is client() for requests with no chat to parse.
func (s *Session) ready() (*whatsmeow.Client, error) {
	cli := s.client()
	if cli.Store.ID == nil {
		return nil, &Error{"unlinked", "link this device first"}
	}
	return cli, nil
}

// inviteErr tells a link or card that no longer works from a failure worth retrying.
func inviteErr(err error) error {
	for _, bad := range []error{whatsmeow.ErrInviteLinkInvalid, whatsmeow.ErrInviteLinkRevoked, whatsmeow.ErrGroupNotFound} {
		if errors.Is(err, bad) {
			return &Error{"invite_invalid", err.Error()}
		}
	}
	// A 401 isn't one of hypermeow's: WhatsApp keeps someone an admin removed from coming back by
	// link (Q10, 2026-11-07, a link to a group the number had been removed from). Retrying won't help.
	if errors.Is(err, whatsmeow.ErrIQNotAuthorized) {
		return &Error{"invite_not_allowed", err.Error()}
	}
	return err
}

type inviteInfo struct {
	Kind   string `json:"kind"`
	Code   string `json:"code"`
	JID    string `json:"jid"`
	Name   string `json:"name"`
	Count  int    `json:"count"`
	Member bool   `json:"member"`
}

func (s *Session) handleInviteInfo(req json.RawMessage) (any, error) {
	var r struct{ URL string }
	if err := json.Unmarshal(req, &r); err != nil {
		return nil, &Error{"bad_request", "invite.info needs url"}
	}
	kind, code, ok := parseInvite(r.URL)
	if !ok {
		return nil, &Error{"bad_request", "not an invite link: " + r.URL}
	}
	cli, err := s.ready()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	if kind == inviteChannel {
		m, err := cli.GetNewsletterInfoWithInvite(ctx, code)
		if err != nil {
			return nil, inviteErr(err)
		}
		return inviteInfo{kind, code, m.ID.String(), m.ThreadMeta.Name.Text, m.ThreadMeta.SubscriberCount, s.store.hasChatRow(m.ID.String())}, nil
	}
	info, err := cli.GetGroupInfoFromLink(ctx, code)
	if err != nil {
		return nil, inviteErr(err)
	}
	n := info.ParticipantCount
	if n == 0 {
		n = len(info.Participants)
	}
	return inviteInfo{kind, code, info.JID.String(), info.Name, n, s.store.hasChatRow(info.JID.String())}, nil
}

func (s *Session) handleInviteAccept(req json.RawMessage) (any, error) {
	var r struct{ Kind, Code, JID, Name string }
	if err := json.Unmarshal(req, &r); err != nil || r.Code == "" {
		return nil, &Error{"bad_request", "invite.accept needs kind and code"}
	}
	if r.Kind != inviteGroup && r.Kind != inviteChannel {
		return nil, &Error{"bad_request", "unknown invite kind: " + r.Kind}
	}
	cli, err := s.ready()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if r.Kind == inviteChannel {
		jid, err := types.ParseJID(r.JID)
		if err != nil || jid.Server != types.NewsletterServer {
			return nil, &Error{"bad_request", "not a channel: " + r.JID}
		}
		if err := cli.FollowNewsletter(ctx, jid); err != nil {
			return nil, inviteErr(err)
		}
		s.store.madeChat(jid.String(), chatNewsletter, r.Name)
		go s.store.channelPosts(cli, jid)
		return map[string]string{"jid": jid.String()}, nil
	}
	jid, err := cli.JoinGroupWithLink(ctx, r.Code)
	if err != nil {
		return nil, inviteErr(err)
	}
	s.store.madeChat(jid.String(), chatGroup, r.Name)
	return map[string]string{"jid": jid.String()}, nil
}

// inviteOf reads an invite card's row: extra has group, code and expires as fillExtra keeps them.
func inviteOf(row msgRow, now int64) (group types.JID, code string, expires int64, err error) {
	var x struct {
		Group   string
		Code    string
		Expires int64
	}
	if json.Unmarshal([]byte(row.Extra), &x) != nil || x.Group == "" || x.Code == "" {
		return group, "", 0, &Error{"invite_invalid", "the card has no invite"}
	}
	if x.Expires > 0 && x.Expires < now {
		return group, "", 0, &Error{"invite_invalid", "the invite has expired"}
	}
	group, err = types.ParseJID(x.Group)
	if err != nil {
		return group, "", 0, &Error{"invite_invalid", "the card's group is not a JID"}
	}
	return group, x.Code, x.Expires, nil
}

func (s *Session) handleGroupJoin(req json.RawMessage) (any, error) {
	var r struct{ Chat, ID string }
	if err := json.Unmarshal(req, &r); err != nil || r.Chat == "" || r.ID == "" {
		return nil, &Error{"bad_request", "group.join needs chat and id"}
	}
	cli, chat, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	row, ok := s.store.row(r.Chat, r.ID)
	if !ok || row.Kind != msgInvite {
		return nil, &Error{"not_found", "no invite card " + r.ID}
	}
	group, code, expires, err := inviteOf(row, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	inviter, err := reactSender(row, chat)
	if err != nil {
		return nil, err
	}
	if inviter.IsEmpty() {
		return nil, &Error{"bad_request", "our own invite card"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if err := cli.JoinGroupWithInvite(ctx, group, inviter, code, expires); err != nil {
		return nil, inviteErr(err)
	}
	s.store.madeChat(group.String(), chatGroup, firstLine(row.Text))
	return map[string]string{"jid": group.String()}, nil
}

// hasChatRow asks the writer whether the chat is in chats.db.
func (st *Store) hasChatRow(jid string) bool {
	c := make(chan bool, 1)
	st.ops <- func(t *storeTx) { c <- t.hasChat(jid) }
	return <-c
}

// madeChat puts a chat just joined in chats.db and returns once that's committed, so the UI finds it when it opens it. Its name and picture may still follow by event.
func (st *Store) madeChat(jid string, kind int, name string) {
	st.ops <- func(t *storeTx) {
		t.ensureChat(jid, kind)
		t.setName(jid, name)
	}
	done := make(chan struct{})
	st.afterCommit(func() { close(done) })
	select {
	case <-done:
	case <-st.done:
	}
}

func registerInvites(srv *Server, s *Session) {
	srv.Handle("invite.info", s.handleInviteInfo)
	srv.Handle("invite.accept", s.handleInviteAccept)
	srv.Handle("group.join", s.handleGroupJoin)
}
