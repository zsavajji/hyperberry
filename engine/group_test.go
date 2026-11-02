package main

import (
	"reflect"
	"testing"

	"github.com/polymorfa/hypermeow/types"
)

func TestCommonGroups(t *testing.T) {
	pn := types.NewJID("4915700000001", types.DefaultUserServer)
	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	other := types.NewJID("4915700000002", types.DefaultUserServer)
	group := func(n, name string, p ...types.GroupParticipant) *types.GroupInfo {
		return &types.GroupInfo{JID: types.NewJID(n, types.GroupServer), GroupName: types.GroupName{Name: name}, Participants: p}
	}
	groups := []*types.GroupInfo{
		group("3", "Gamma", types.GroupParticipant{JID: pn}),
		group("2", "Other", types.GroupParticipant{JID: other}),
		group("1", "beta", types.GroupParticipant{JID: other, LID: lid}),
	}
	want := []groupRef{{"1@g.us", "beta"}, {"3@g.us", "Gamma"}}
	if got := commonGroups(groups, lid.User, pn.User); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := commonGroups(groups, "", ""); got == nil || len(got) != 0 {
		t.Errorf("no user: %#v, want an empty array", got)
	}
}

func TestParticipantReq(t *testing.T) {
	chat, jid, a, err := participantReq([]byte(`{"chat":"1@g.us","jid":"2@lid","action":"promote"}`))
	if err != nil || chat != "1@g.us" || jid != "2@lid" || a != "promote" {
		t.Errorf("got %q %q %q %v", chat, jid, a, err)
	}
	if _, _, _, err := participantReq([]byte(`{"chat":"1@g.us","jid":"2@lid","action":"kick"}`)); err == nil {
		t.Error("kick: no error")
	}
}
