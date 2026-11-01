package main

import (
	"context"

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
	var ids []types.JID
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
	var infos []types.ContactInfo
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
				return n
			}
		}
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
