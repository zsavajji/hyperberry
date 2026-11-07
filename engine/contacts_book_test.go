package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/polymorfa/hypermeow/types"
)

func newCache() *waCache { return &waCache{seen: map[string]types.JID{}} }

func TestBookContacts(t *testing.T) {
	cands := map[string]string{"393331111111": "Anna", "393332222222": "Bruno", "393333333333": "Carla"}
	calls := 0
	ask := func(p []string) ([]types.IsOnWhatsAppResponse, error) {
		calls++
		return []types.IsOnWhatsAppResponse{
			{Query: p[0], IsIn: true, JID: types.NewJID("393331111111", types.DefaultUserServer)},
			{Query: "+39 333 2222222", IsIn: false},
			// no Query: matched by its phone number
			{IsIn: true, JID: types.NewJID("99", types.HiddenUserServer), PhoneNumber: types.NewJID("393333333333", types.DefaultUserServer)},
		}, nil
	}
	cache := newCache()
	got, err := bookContacts(cands, cache, ask)
	if err != nil || len(got) != 2 || got[0].Name != "Anna" || got[1].Name != "Carla" || got[1].PN.User != "393333333333" {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := bookContacts(cands, cache, ask); err != nil || calls != 1 {
		t.Errorf("asked again: %d calls, %v", calls, err)
	}
}

func TestBookContactsError(t *testing.T) {
	cache := newCache()
	got, err := bookContacts(map[string]string{"393331111111": "Anna"}, cache, func([]string) ([]types.IsOnWhatsAppResponse, error) {
		return nil, errors.New("offline")
	})
	if err == nil || len(got) != 0 || len(cache.seen) != 0 {
		t.Errorf("got %v, %v, cached %v", got, err, cache.seen)
	}
}

func TestBookContactsBatches(t *testing.T) {
	cands := map[string]string{}
	for i := 0; i < 450; i++ {
		cands[fmt.Sprintf("39333%07d", i)] = "X"
	}
	var sizes []int
	bookContacts(cands, newCache(), func(p []string) ([]types.IsOnWhatsAppResponse, error) {
		sizes = append(sizes, len(p))
		return nil, nil
	})
	if fmt.Sprint(sizes) != "[200 200 50]" {
		t.Errorf("batches %v", sizes)
	}
}
