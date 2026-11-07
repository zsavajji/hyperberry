package main

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/polymorfa/hypermeow"
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

// contactKey is the key of a phone JID's 1:1 chat: its LID when the store knows one, as chatKey
// does, but without the merge: this only reads.
func contactKey(ctx context.Context, cli *whatsmeow.Client, jid types.JID) string {
	if jid.Server == types.DefaultUserServer {
		if lid, err := cli.Store.LIDs.GetLIDForPN(ctx, jid); err == nil && lid.Server == types.HiddenUserServer {
			return lid.ToNonAD().String()
		}
	}
	return jid.String()
}

// phoneName is a number of the phone's book that has WhatsApp.
type phoneName struct {
	PN   types.JID
	Name string
}

// waCache remembers what IsOnWhatsApp said of each number (digits, country code first), an empty
// JID for not registered.
type waCache struct {
	mu   sync.Mutex
	seen map[string]types.JID
}

// onWhatsApp remembers what IsOnWhatsApp said of each number, "" for not registered, so the picker
// asks only about numbers it hasn't asked about. For the process's life: a number's registration
// rarely changes, and an engine start asks again.
var onWhatsApp = &waCache{seen: map[string]types.JID{}}

// askBatch is how many numbers go to IsOnWhatsApp at once.
const askBatch = 200

// bookContacts are the book's numbers that have WhatsApp, named as the book names them. ask is
// IsOnWhatsApp, asked only about numbers the cache doesn't know, in batches of at most 200. A failed ask
// is logged by the caller and leaves those numbers out until the next list.
func bookContacts(cands map[string]string, cache *waCache, ask func(phones []string) ([]types.IsOnWhatsAppResponse, error)) ([]phoneName, error) {
	nums := make([]string, 0, len(cands))
	for n := range cands {
		nums = append(nums, n)
	}
	sort.Strings(nums)
	cache.mu.Lock()
	var todo []string
	for _, n := range nums {
		if _, ok := cache.seen[n]; !ok {
			todo = append(todo, n)
		}
	}
	cache.mu.Unlock()
	var err error
	for len(todo) > 0 && err == nil {
		batch := todo[:min(askBatch, len(todo))]
		todo = todo[len(batch):]
		phones := make([]string, len(batch))
		for i, n := range batch {
			phones[i] = "+" + n
		}
		var resp []types.IsOnWhatsAppResponse
		if resp, err = ask(phones); err != nil {
			break
		}
		cache.mu.Lock()
		for _, r := range resp {
			pn := r.PhoneNumber
			if pn.Server != types.DefaultUserServer && r.JID.Server == types.DefaultUserServer {
				pn = r.JID
			}
			n := strings.NewReplacer("+", "", " ", "").Replace(r.Query)
			if _, ok := cands[n]; !ok {
				n = pn.User
			}
			if _, ok := cands[n]; !ok {
				continue
			}
			if r.IsIn && pn.Server == types.DefaultUserServer {
				cache.seen[n] = pn
			} else if !r.IsIn {
				cache.seen[n] = types.JID{}
			}
		}
		cache.mu.Unlock()
	}
	var out []phoneName
	cache.mu.Lock()
	for _, n := range nums {
		if pn := cache.seen[n]; !pn.IsEmpty() {
			out = append(out, phoneName{pn, cands[n]})
		}
	}
	cache.mu.Unlock()
	return out, err
}

// handleContacts is contacts.list: WhatsApp's saved contacts (the main phone's book) plus the
// numbers of the Q10's own address book that WhatsApp says are registered (IsOnWhatsApp, asked once
// per number per engine run, see onWhatsApp). Both are keyed as the 1:1 chat is (contactKey), and
// named by the Q10's book first, as names.go does. A failed ask leaves the book's numbers out.
func (s *Session) handleContacts(json.RawMessage) (any, error) {
	cli := s.client()
	if cli.Store.ID == nil {
		return nil, &Error{"unlinked", "link this device first"}
	}
	// sendTimeout: IsOnWhatsApp is a round trip on top of the store reads.
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	all, err := cli.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return nil, err
	}
	own := cli.Store.ID.User
	out, seen := []contact{}, map[string]bool{}
	for jid, ci := range all {
		jid = jid.ToNonAD()
		name := savedName(ci)
		if name == "" || (jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer) ||
			isUs(cli.Store.ID, jid) || isUs(&cli.Store.LID, jid) {
			continue
		}
		pn := jid
		if jid.Server == types.HiddenUserServer {
			pn, _ = cli.Store.LIDs.GetPNForLID(ctx, jid)
		}
		if pn.Server == types.DefaultUserServer {
			if n := phoneBook.name(pn.User, own); n != "" {
				name = n
			}
		}
		if key := contactKey(ctx, cli, jid); !seen[key] {
			seen[key] = true
			out = append(out, contact{JID: key, Name: name})
		}
	}
	phones, err := bookContacts(phoneBook.candidates(own), onWhatsApp, func(p []string) ([]types.IsOnWhatsAppResponse, error) {
		return cli.IsOnWhatsApp(ctx, p)
	})
	if err != nil {
		s.log.Warnf("contacts: the book's numbers: %v", err)
	}
	for _, p := range phones {
		if key := contactKey(ctx, cli, p.PN); !seen[key] && !isUs(cli.Store.ID, p.PN) && !isUs(&cli.Store.LID, p.PN) {
			seen[key] = true
			out = append(out, contact{JID: key, Name: p.Name})
		}
	}
	sortContacts(out)
	return out, nil
}
