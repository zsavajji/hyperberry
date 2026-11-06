package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
)

// Channels (step 6.6g): the followed ones and their last posts, fetched on every connect and when the Channels page or a channel opens; the server's pushes go in through the usual message path, unread but silent. WhatsApp keeps channels apart under Updates, and so do we.

// channelPage is how many posts a fetch asks for.
const channelPage = 30

func isChannel(key string) bool { return strings.HasSuffix(key, "@"+types.NewsletterServer) }

// syncChannels lists the followed channels off the writer, since it is a network call, and fetches each one's last posts.
// ponytail: every channel on every connect; only on opening them if someone follows dozens.
func (st *Store) syncChannels(cli *whatsmeow.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	list, err := cli.GetSubscribedNewsletters(ctx)
	if err != nil {
		st.log.Warnf("channels: %v", err)
		return
	}
	st.ops <- func(t *storeTx) {
		followed := map[string]bool{}
		for _, c := range list {
			key := c.ID.String()
			followed[key] = true
			t.ensureChat(key, chatNewsletter)
			t.setName(key, c.ThreadMeta.Name.Text)
		}
		for _, key := range t.channelKeys() {
			if !followed[key] { // unfollowed on another device
				t.clearChat(key, true)
			}
		}
	}
	for _, c := range list {
		st.channelPosts(cli, c.ID)
	}
}

// channelPosts fetches a channel's last posts and queues them.
func (st *Store) channelPosts(cli *whatsmeow.Client, jid types.JID) {
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	posts, err := cli.GetNewsletterMessages(ctx, jid, &whatsmeow.GetNewsletterMessagesParams{Count: channelPage})
	if err != nil {
		st.log.Warnf("channel %s: %v", jid, err)
		return
	}
	st.ops <- func(t *storeTx) { st.writePosts(context.Background(), t, cli, jid, posts) }
}

// writePosts stores the posts as messages. A fetch is history to apply (no alert, no download), but the posts new to a channel we already had are unread; a channel's first fetch isn't news.
func (st *Store) writePosts(ctx context.Context, t *storeTx, cli *whatsmeow.Client, jid types.JID, posts []*types.NewsletterMessage) {
	key := jid.String()
	before := t.messageCount(key)
	for _, p := range posts {
		if p.Message == nil {
			continue
		}
		e := (&events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: jid, Sender: jid},
			ID: p.MessageID, ServerID: p.MessageServerID, Timestamp: p.Timestamp, Type: p.Type}, RawMessage: p.Message}).UnwrapRaw()
		st.apply(ctx, t, cli, key, e, false)
	}
	if n := t.messageCount(key) - before; before > 0 && n > 0 {
		t.addUnread(key, n)
	}
}

func (t *storeTx) messageCount(chat string) int {
	var n int
	t.tx.QueryRow("SELECT COUNT(*) FROM messages WHERE chat = ?", chat).Scan(&n)
	return n
}

func (t *storeTx) channelKeys() []string {
	rows, err := t.tx.Query("SELECT jid FROM chats WHERE kind = ?", chatNewsletter)
	if err != nil {
		t.log.Warnf("channels: %v", err)
		return nil
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			keys = append(keys, k)
		}
	}
	return keys
}

func (s *Session) handleChannelsSync(json.RawMessage) (any, error) {
	cli, err := s.ready()
	if err != nil {
		return nil, err
	}
	go s.store.syncChannels(cli)
	return nil, nil
}

func (s *Session) handleChannelUnfollow(req json.RawMessage) (any, error) {
	var r struct{ Chat string }
	json.Unmarshal(req, &r)
	cli, jid, err := s.linked(r.Chat)
	if err != nil {
		return nil, err
	}
	if jid.Server != types.NewsletterServer {
		return nil, &Error{"bad_chat", "not a channel: " + r.Chat}
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if err := cli.UnfollowNewsletter(ctx, jid); err != nil {
		return nil, err
	}
	s.store.ops <- func(t *storeTx) { t.clearChat(r.Chat, true) }
	return nil, nil
}

func registerChannels(srv *Server, s *Session) {
	srv.Handle("channels.sync", s.handleChannelsSync)
	srv.Handle("channel.unfollow", s.handleChannelUnfollow)
}
