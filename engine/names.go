package main

import (
	"context"
	"database/sql"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
)

// displayName is the name to show for a person: the contact name, else the push name, else
// the phone number. alt is the other address (phone or LID) the caller knows, if any.
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

// storedName is the name the contact store holds for a person (saved name, business name or
// push name), "" for none: no phone-number placeholder, so it never replaces a push name
// a message brought.
func storedName(ctx context.Context, cli *whatsmeow.Client, jid, alt types.JID) string {
	name, _, _ := lookupName(ctx, cli, jid, alt)
	return name
}

// lookupName is storedName with the addresses and contact cards it looked at, for resolveName's
// fallbacks.
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
