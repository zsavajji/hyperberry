package main

import (
	"testing"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

func TestHoldLive(t *testing.T) {
	n := newNotifier(t.TempDir(), waLog.Noop)
	defer n.close()
	st := &Store{nt: n}
	old := onCellular
	t.Cleanup(func() { onCellular = old })
	photo, sticker := msgRow{Kind: msgPhoto}, msgRow{Kind: msgSticker}
	for _, c := range []struct {
		mobile, cell, photo, sticker bool
	}{
		{false, true, true, false},   // off on mobile data: the photo waits
		{false, false, false, false}, // off on Wi-Fi
		{true, true, false, false},   // on
		{true, false, false, false},
	} {
		onCellular = func() bool { return c.cell }
		if _, err := n.setPrefs([]byte(`{"mediaOnMobile":` + map[bool]string{true: "true", false: "false"}[c.mobile] + `}`)); err != nil {
			t.Fatal(err)
		}
		if got := st.holdLive(photo); got != c.photo {
			t.Errorf("%+v: photo held %v", c, got)
		}
		if got := st.holdLive(sticker); got != c.sticker {
			t.Errorf("%+v: sticker held %v", c, got)
		}
	}
}
