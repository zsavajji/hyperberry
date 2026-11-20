package main

import (
	"testing"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

func TestGoMemLimit(t *testing.T) {
	for mb, want := range map[int]int64{0: 24 << 20, 30: 24 << 20, 128: 107374180} {
		if got := goMemLimit(mb); got != want {
			t.Errorf("%d MiB: %d, want %d", mb, got, want)
		}
	}
	if goMemLimit(128)>>20 != 102 {
		t.Error("128 MiB is not 102 MiB")
	}
}

// The test process has some limit or none; reading must not fail or raise it above the hard one.
func TestReadMemMB(t *testing.T) {
	if n := readMemMB(waLog.Noop); n < 0 {
		t.Errorf("%d", n)
	}
}

func TestLinkBodyMemMB(t *testing.T) {
	old := memMB
	defer func() { memMB = old }()
	memMB = 30
	s, _, _ := newTestSim(t)
	for _, link := range []string{linkUnlinked, linkQR, linkReady} {
		s.link = link
		if b := s.body(); b["memMB"] != 30 {
			t.Errorf("%s: %v", link, b)
		}
	}
}
