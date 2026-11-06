package main

import (
	"errors"
	"testing"

	"github.com/polymorfa/hypermeow"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

func TestConfigureReconnect(t *testing.T) {
	_, cli := historyFixture(t)
	configureReconnect(cli)
	if !cli.InitialAutoReconnect {
		t.Fatal("a first connect would not retry")
	}
	for _, c := range []struct{ in, want int }{{3, 3}, {500, reconnectCap}} {
		cli.AutoReconnectErrors = c.in
		if !cli.AutoReconnectHook(errors.New("x")) {
			t.Fatal("hook says stop")
		}
		if cli.AutoReconnectErrors != c.want {
			t.Errorf("errors %d became %d, want %d", c.in, cli.AutoReconnectErrors, c.want)
		}
	}
}

func TestConnectUntil(t *testing.T) {
	never := func() bool { return false }
	t.Run("retries then succeeds", func(t *testing.T) {
		n := 0
		connectUntil(func() error {
			if n++; n < 3 {
				return errors.New("x")
			}
			return nil
		}, never, 0, waLog.Noop)
		if n != 3 {
			t.Errorf("%d calls, want 3", n)
		}
	})
	t.Run("already connected ends it", func(t *testing.T) {
		n := 0
		connectUntil(func() error { n++; return whatsmeow.ErrAlreadyConnected }, never, 0, waLog.Noop)
		if n != 1 {
			t.Errorf("%d calls, want 1", n)
		}
	})
	t.Run("stale from the start", func(t *testing.T) {
		n := 0
		connectUntil(func() error { n++; return nil }, func() bool { return true }, 0, waLog.Noop)
		if n != 0 {
			t.Errorf("%d calls, want 0", n)
		}
	})
	t.Run("stale after a failure", func(t *testing.T) {
		n := 0
		connectUntil(func() error { n++; return errors.New("x") }, func() bool { return n >= 1 }, 0, waLog.Noop)
		if n != 1 {
			t.Errorf("%d calls, want 1", n)
		}
	})
}
