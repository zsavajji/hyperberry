package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
)

// displayName is the name to show for a person: the name in the phone's own address book, else
// WhatsApp's contact name, else the push name, else the phone number. alt is the other address
// (phone or LID) the caller knows, if any.
func displayName(ctx context.Context, cli *whatsmeow.Client, jid, alt types.JID) string {
	return resolveName(ctx, cli, jid, alt, "")
}

// resolveName is displayName with a push name from the message in hand, which beats the
// phone-number fallback.
func resolveName(ctx context.Context, cli *whatsmeow.Client, jid, alt types.JID, push string) string {
	// WhatsApp's own account (its tips and notices) has no contact card, so it would be "+0".
	if j := jid.ToNonAD(); j == types.PSAJID || j == types.LegacyPSAJID {
		return "WhatsApp"
	}
	name, ids, infos := lookupName(ctx, cli, jid, alt)
	if name != "" {
		return name
	}
	if push != "" {
		return push
	}
	for _, j := range ids {
		if j.Server == types.DefaultUserServer {
			return "+" + j.User
		}
	}
	for _, ci := range infos {
		if ci.RedactedPhone != "" {
			return ci.RedactedPhone
		}
	}
	// Nothing known: "", which setName skips, so a name found earlier is never overwritten
	// by a placeholder. The UI draws its own placeholder for a chat without a name.
	return ""
}

// storedName is the name the phone's address book or the contact store holds for a person (saved
// name, business name or push name), "" for none: no phone-number placeholder, so it never
// replaces a push name a message brought.
func storedName(ctx context.Context, cli *whatsmeow.Client, jid, alt types.JID) string {
	name, _, _ := lookupName(ctx, cli, jid, alt)
	return name
}

// lookupName is storedName with the addresses and contact cards it looked at, for resolveName's
// fallbacks. The phone's book comes first, by any phone number the person has.
func lookupName(ctx context.Context, cli *whatsmeow.Client, jid, alt types.JID) (name string, ids []types.JID, infos []types.ContactInfo) {
	for _, j := range []types.JID{jid.ToNonAD(), alt.ToNonAD()} {
		if j.IsEmpty() {
			continue
		}
		ids = append(ids, j)
		// ponytail: one counterpart lookup per address; hypermeow caches them.
		switch j.Server {
		case types.DefaultUserServer:
			if l, err := cli.Store.LIDs.GetLIDForPN(ctx, j); err == nil && !l.IsEmpty() {
				ids = append(ids, l)
			}
		case types.HiddenUserServer:
			if p, err := cli.Store.LIDs.GetPNForLID(ctx, j); err == nil && !p.IsEmpty() {
				ids = append(ids, p)
			}
		}
	}
	for _, j := range ids {
		if ci, err := cli.Store.Contacts.GetContact(ctx, j); err == nil {
			infos = append(infos, ci)
		}
	}
	own := ""
	if cli.Store.ID != nil { // nil while unlinked
		own = cli.Store.ID.User
	}
	for _, j := range ids {
		if j.Server == types.DefaultUserServer {
			if n := phoneBook.name(j.User, own); n != "" {
				return n, ids, infos
			}
		}
	}
	for _, pick := range []func(types.ContactInfo) string{
		func(c types.ContactInfo) string { return c.FullName },
		func(c types.ContactInfo) string { return c.FirstName },
		func(c types.ContactInfo) string { return c.BusinessName },
		func(c types.ContactInfo) string { return c.PushName },
	} {
		for _, ci := range infos {
			if n := pick(ci); n != "" {
				return n, ids, infos
			}
		}
	}
	return "", ids, infos
}

// renameAll gives the saved name to everyone we have a 1:1 chat with or a group message from,
// for the names that arrive without an event (the address book a link brings in). It reads
// the current names first and writes only what differs: messages has no index on sender, so an
// UPDATE per person would scan the table per person. Only the contact store's names count, so a
// push name from a message is never replaced by a phone number.
// ponytail: two reads and one contact lookup per person on every connect, no writes once the
// names agree; fine for hundreds, an "only after a contacts sync" flag if it shows on a big account.
func (st *Store) renameAll(ctx context.Context, t *storeTx, cli *whatsmeow.Client) {
	chats := map[string]string{}     // 1:1 chat -> name
	senders := map[string][]string{} // group sender -> the names its messages carry
	read := func(q string, args []any, row func(k string, name sql.NullString)) {
		rows, err := t.tx.Query(q, args...)
		if err != nil {
			t.log.Warnf("rename all: %v", err)
			return
		}
		defer rows.Close() // closed before any write below
		for rows.Next() {
			var k string
			var n sql.NullString
			if rows.Scan(&k, &n) == nil {
				row(k, n)
			}
		}
	}
	read("SELECT jid, name FROM chats WHERE kind = ?", []any{chatUser}, func(k string, n sql.NullString) {
		chats[k] = n.String
	})
	read("SELECT DISTINCT sender, sender_name FROM messages WHERE sender IS NOT NULL AND sender != ''", nil,
		func(k string, n sql.NullString) { senders[k] = append(senders[k], n.String) })

	people := map[string]bool{}
	for k := range chats {
		people[k] = true
	}
	for k := range senders {
		people[k] = true
	}
	for k := range people {
		j, err := types.ParseJID(k)
		if err != nil || j.IsEmpty() {
			continue
		}
		name := storedName(ctx, cli, j, types.EmptyJID)
		if name == "" {
			continue
		}
		if cur, ok := chats[k]; ok && cur != name {
			t.setName(k, name)
		}
		for _, sn := range senders[k] {
			if sn != name { // a NULL reads as "", which differs
				t.renameSender(k, name)
				break
			}
		}
	}
}

// renameSender gives a person's name to the group messages they sent, whose sender_name is
// written once at store time.
func (t *storeTx) renameSender(key, name string) {
	t.exec("UPDATE messages SET sender_name = ?1 WHERE sender = ?2 AND sender_name IS NOT ?1", name, key)
}

// mention is a person an "@" in a message's text points at: their key as chats and group
// senders have it (the page the UI opens), "" for ourselves, and the name the text now carries.
type mention struct {
	JID  string `json:"jid"`
	Name string `json:"name"`
}

// mentionNames turns the "@<number>" WhatsApp sends for each mentioned person into "@name", and
// lists the people it named. A mention whose "@<number>" isn't in the text is dropped.
func mentionNames(ctx context.Context, t *storeTx, cli *whatsmeow.Client, ci *waE2E.ContextInfo, text string) (string, []mention) {
	var ms []mention
	byUser := map[string]string{}
	for _, s := range ci.GetMentionedJID() {
		j, err := types.ParseJID(s)
		if _, dup := byUser[j.User]; err != nil || j.IsEmpty() || dup || !strings.Contains(text, "@"+j.User) {
			continue
		}
		m := mention{}
		if isUs(cli.Store.ID, j) || isUs(&cli.Store.LID, j) {
			m.Name = cli.Store.PushName // no link: our own page is the profile's
		} else {
			m.JID = chatKey(ctx, t, cli.Store.LIDs, j, types.EmptyJID)
			m.Name = resolveName(ctx, cli, j, types.EmptyJID, "")
		}
		if m.Name == "" {
			m.Name = j.User
		}
		byUser[j.User] = m.Name
		ms = append(ms, m)
	}
	return atNames(text, byUser), ms
}

// atNames replaces each "@<user>" of text with "@<name>". The longest user goes first, so
// "@123" never eats the start of "@1234".
func atNames(text string, byUser map[string]string) string {
	users := make([]string, 0, len(byUser))
	for u := range byUser {
		users = append(users, u)
	}
	sort.Slice(users, func(i, j int) bool { return len(users[i]) > len(users[j]) })
	var pairs []string // a Replacer tries these in order, in one pass over text
	for _, u := range users {
		pairs = append(pairs, "@"+u, "@"+byUser[u])
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

// mentionWire turns the text of a message we send ("@Anna Lee", as the bubble shows it) into what
// WhatsApp takes: "@<number>", the user part of the person's JID, and lists those JIDs. A mention
// whose JID doesn't parse, or whose "@name" isn't in the text any more, is skipped.
// ponytail: the JIDs are the UI's chat keys, LID first, sent even in a group addressed by phone
// number; send the phone number there if Android shows such a mention wrong.
func mentionWire(text string, ms []mention) (wire string, jids []string) {
	sorted := append([]mention(nil), ms...) // longest name first, so "@Ann" never takes the start of "@Anna"
	sort.SliceStable(sorted, func(i, j int) bool { return len(sorted[i].Name) > len(sorted[j].Name) })
	var pairs []string // one pass over the text, in this order
	seen := map[string]bool{}
	for _, m := range sorted {
		j, err := types.ParseJID(m.JID)
		if err != nil || j.IsEmpty() || m.Name == "" || !strings.Contains(text, "@"+m.Name) {
			continue
		}
		pairs = append(pairs, "@"+m.Name, "@"+j.User)
		if !seen[j.String()] {
			seen[j.String()] = true
			jids = append(jids, j.String())
		}
	}
	if len(pairs) == 0 {
		return text, nil
	}
	return strings.NewReplacer(pairs...).Replace(text), jids
}

// withMentions adds the mentions to a row's extra, a JSON object or "". Other keys stay as they were.
func withMentions(extra string, ms []mention) string {
	x := map[string]json.RawMessage{}
	if extra != "" && json.Unmarshal([]byte(extra), &x) != nil {
		return extra
	}
	b, err := json.Marshal(ms)
	if err != nil {
		return extra
	}
	x["mentions"] = b
	if b, err = json.Marshal(x); err != nil {
		return extra
	}
	return string(b)
}
