package main

import (
	"context"
	"testing"

	"github.com/polymorfa/hypermeow/types"
)

func TestWhatsAppAccountName(t *testing.T) {
	ctx, cli := context.Background(), newUnlinked(t).cli
	for _, j := range []types.JID{types.PSAJID, types.LegacyPSAJID} {
		if n := displayName(ctx, cli, j, types.EmptyJID); n != "WhatsApp" {
			t.Errorf("%s: %q", j, n)
		}
		if n := resolveName(ctx, cli, j, types.EmptyJID, "a push name"); n != "WhatsApp" {
			t.Errorf("%s with a push name: %q", j, n)
		}
	}
}
