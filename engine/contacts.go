package main

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/polymorfa/hypermeow/types"
)

type contact struct {
	JID  string `json:"jid"`
	Name string `json:"name"`
}

func registerContacts(srv *Server, s *Session) {
	srv.Handle("contacts.list", s.handleContacts)
	srv.Handle("contacts.book", s.handleBook)
}

// handleBook is contacts.book: the phone's address book, which names people before WhatsApp's
// does. It works unlinked (the UI sends it on every connect); once linked, a changed book renames
// the chats and group messages at once. An empty list is the UI's decision and is taken as such.
// Only counts are logged: the book is names and numbers.
func (s *Session) handleBook(req json.RawMessage) (any, error) {
	var r struct{ Entries []bookEntry }
	if err := json.Unmarshal(req, &r); err != nil {
		return nil, &Error{"bad_request", "contacts.book needs entries"}
	}
	changed := phoneBook.set(r.Entries)
	s.log.Infof("contact book: %d numbers, changed %v", len(r.Entries), changed)
	if cli := s.client(); changed && cli.Store.ID != nil {
		s.store.ops <- func(t *storeTx) { s.store.renameAll(context.Background(), t, cli) }
	}
	return nil, nil
}

// sortContacts orders by name, ignoring case.
func sortContacts(c []contact) {
	sort.SliceStable(c, func(i, j int) bool {
		return strings.ToLower(c[i].Name) < strings.ToLower(c[j].Name)
	})
}

// savedName is the name in the address book: the full name, else the first, else the business
// name. A push name isn't one: everyone who ever wrote to us has it.
func savedName(c types.ContactInfo) string {
	for _, n := range []string{c.FullName, c.FirstName, c.BusinessName} {
		if n != "" {
			return n
		}
	}
	return ""
}

// handleContacts is contacts.list: the saved contacts, keyed as their 1:1 chat is (LID when
// known, as chatKey does, but without the merge: this only reads).
func (s *Session) handleContacts(json.RawMessage) (any, error) {
	cli := s.client()
	if cli.Store.ID == nil {
		return nil, &Error{"unlinked", "link this device first"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	all, err := cli.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return nil, err
	}
	out, seen := []contact{}, map[string]bool{}
	for jid, ci := range all {
		jid = jid.ToNonAD()
		name := savedName(ci)
		if name == "" || (jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer) ||
			isUs(cli.Store.ID, jid) || isUs(&cli.Store.LID, jid) {
			continue
		}
		key := jid
		if jid.Server == types.DefaultUserServer {
			if lid, err := cli.Store.LIDs.GetLIDForPN(ctx, jid); err == nil && lid.Server == types.HiddenUserServer {
				key = lid.ToNonAD()
			}
		}
		if !seen[key.String()] {
			seen[key.String()] = true
			out = append(out, contact{JID: key.String(), Name: name})
		}
	}
	sortContacts(out)
	return out, nil
}
