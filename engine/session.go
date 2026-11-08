package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/store/sqlstore"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"github.com/purpshell/meowcaller"
)

// Link states (PLAN.md §5). "syncing" is left to the store, which knows when history is in.
const (
	linkUnlinked = "unlinked"
	linkQR       = "qr"
	linkCode     = "code"
	linkLinking  = "linking"
	linkReady    = "ready"
)

// Session owns the hypermeow client and the link state machine. When the device is removed,
// by the phone or by link.logout, hypermeow deletes it from session.db, so the next link
// starts with a fresh device and a fresh client.
type Session struct {
	container *sqlstore.Container
	srv       *Server
	store     *Store
	log       waLog.Logger

	mu     sync.Mutex
	cli    *whatsmeow.Client
	link   string
	conn   string        // offline, connecting, ready
	qr     string        // the current QR code, in state qr
	code   string        // the pairing code, in state code
	stopQR func()        // ends the running QR channel
	qrSeen chan struct{} // closed when the QR channel's first code arrives
	rec    voiceRec      // the voice note being recorded
	// syncing is true from a new link until its recent chats are in chats.db, or syncCap: the
	// UI waits on the linking page meanwhile (the user's, 2026-11-01).
	syncing bool
	photos  *photoQueue                       // the chats' pictures (photos.go)
	calls   *Calls                            // the live call (calls.go)
	mc      atomic.Pointer[meowcaller.Client] // meowcaller on the current client
}

// syncCap ends "syncing" if the phone sends no recent-chats history: an account with no chats.
// ponytail: a guess, untested until the real link.
var syncCap = 90 * time.Second

func newSession(container *sqlstore.Container, srv *Server, store *Store, log waLog.Logger) (*Session, error) {
	device, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, err
	}
	s := &Session{container: container, srv: srv, store: store, log: log, link: linkUnlinked, conn: "offline"}
	s.initCalls()
	s.photos = newPhotoQueue(store, func() photoFetch { return clientFetch(s.client()) })
	store.historyIn = func() { go s.endSync() } // off the writer: endSync takes s.mu
	s.cli = s.newClient(device)
	if s.cli.Store.ID != nil {
		s.link, s.conn = linkReady, "connecting"
	}
	srv.Handle("link.qr", s.handleQR)
	srv.Handle("link.code", s.handleCode)
	srv.Handle("link.logout", s.handleLogout)
	registerSend(srv, s)
	registerPresence(srv, s)
	registerGroup(srv, s)
	registerInvites(srv, s)
	registerChannels(srv, s)
	registerContacts(srv, s)
	registerProfile(srv, s)
	registerMedia(srv, s)
	registerVoiceSend(srv, s)
	srv.Handle("media.send", s.handleMediaSend)
	if s.cli.Store.ID != nil {
		// Not waited for: the UI gets its answers at once, whatever the network is doing.
		cli := s.cli
		go connectUntil(cli.Connect, func() bool { return cli != s.client() }, time.Minute, s.log)
	}
	return s, nil
}

// reconnectCap is the failures at which hypermeow's wait, 2 s for each, stops growing: 2 minutes.
// Left alone, three days offline means 17 minutes before it looks again.
const reconnectCap = 60

// configureReconnect has a first connect with no network retry as a later one does. hypermeow's
// default returns the error, which a reboot with no network would meet every time.
func configureReconnect(cli *whatsmeow.Client) {
	cli.InitialAutoReconnect = true
	cli.AutoReconnectHook = func(error) bool {
		cli.AutoReconnectErrors = min(cli.AutoReconnectErrors, reconnectCap)
		return true
	}
}

// connectUntil runs the first connect. A network failure doesn't reach here (configureReconnect),
// but anything else would leave the engine connecting for ever, so it tries again. stale says a logout replaced this client.
func connectUntil(connect func() error, stale func() bool, pause time.Duration, log waLog.Logger) {
	for !stale() {
		err := connect()
		if err == nil || errors.Is(err, whatsmeow.ErrAlreadyConnected) {
			return
		}
		log.Errorf("connect: %v", err)
		time.Sleep(pause)
	}
}

func (s *Session) newClient(device *store.Device) *whatsmeow.Client {
	cli := whatsmeow.NewClient(device, s.log.Sub("client"))
	// hypermeow would acknowledge a history chunk before downloading it and then read it whole.
	// It only dispatches the notification now; the Store works it and sends the receipt itself.
	cli.ManualHistorySyncDownload = true
	cli.DisableManualHistorySyncReceipt = true
	configureReconnect(cli)
	s.callsOn(cli) // before it connects: meowcaller listens to the call stanzas from the start
	cli.AddEventHandler(func(evt any) { s.onEvent(cli, evt) })
	cli.AddEventHandler(func(evt any) { s.store.handle(cli, evt) })
	return cli
}

// client returns the current client; a logout replaces it, so take it once per request.
func (s *Session) client() *whatsmeow.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cli
}

// openChat is the chat key on screen, or "" (step 5.1's notifier skips it).
func (s *Session) openChat() string {
	return s.store.ui.openChat()
}

// hello is the link part of the hello reply.
func (s *Session) hello() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.linkBody()
}

func (s *Session) linkBody() map[string]any {
	b := map[string]any{"state": s.link, "conn": s.conn}
	if s.syncing {
		b["syncing"] = true
	}
	switch {
	case s.link == linkQR:
		b["qr"] = s.qr
	case s.link == linkCode:
		b["code"] = s.code
	case s.cli.Store.ID != nil:
		b["me"] = s.cli.Store.ID.ToNonAD().String()
		b["name"] = s.cli.Store.PushName
	}
	return b
}

// setLink changes the link state and tells the UI. Call it with s.mu held.
func (s *Session) setLink(state string) {
	s.link = state
	s.srv.Emit("link", s.linkBody())
}

// endSync ends a new link's "syncing" and tells the UI, then lets the Hub show the account.
func (s *Session) endSync() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.syncing {
		s.syncing = false
		s.setLink(s.link)
		s.store.nt.link() // the new link's chats are in
		s.photos.round()  // and need their pictures
	}
}

func (s *Session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopQR != nil {
		s.stopQR()
	}
	s.cli.Disconnect()
}

// startQR opens the login websocket with a QR channel, unless one is already running.
// Call it with s.mu held.
func (s *Session) startQR() error {
	if s.stopQR != nil {
		return nil
	}
	if s.cli.IsConnected() {
		s.cli.Disconnect() // a QR channel has to be opened before connecting
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := s.cli.GetQRChannel(ctx)
	if err == nil {
		err = s.cli.Connect()
	}
	if err != nil {
		cancel()
		return err
	}
	s.stopQR, s.qrSeen = cancel, make(chan struct{})
	go s.readQR(s.cli, ch, s.qrSeen)
	return nil
}

func (s *Session) readQR(cli *whatsmeow.Client, ch <-chan whatsmeow.QRChannelItem, seen chan struct{}) {
	first := true
	for item := range ch {
		s.mu.Lock()
		if cli != s.cli {
			s.mu.Unlock()
			return
		}
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			if first {
				first = false
				close(seen)
			}
			if s.link != linkCode {
				s.qr = item.Code
				s.setLink(linkQR)
			}
		case "success":
			s.setLink(linkLinking)
		default: // timeout, or an error: the login socket is gone, so start over
			s.log.Warnf("QR channel ended: %s %v", item.Event, item.Error)
			s.stopQR, s.qr, s.code = nil, "", ""
			if s.link != linkLinking && s.link != linkReady {
				s.setLink(linkUnlinked)
			}
		}
		s.mu.Unlock()
	}
}

func (s *Session) handleQR(json.RawMessage) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.link == linkReady || s.link == linkLinking {
		return nil, &Error{"linked", "this device is already linked"}
	}
	if err := s.startQR(); err != nil {
		return nil, err
	}
	if s.qr != "" {
		return map[string]string{"qr": s.qr}, nil
	}
	return nil, nil // the codes come as link events
}

// pairDigits is link.code's phone number as PairPhone takes it: digits only, with the country
// code. "0039…" is how much of Europe dials "+39…". The rest is PairPhone's own check, done
// first so a bad number doesn't cost the running QR code.
func pairDigits(phone string) (string, error) {
	digits := strings.Map(func(c rune) rune {
		if c >= '0' && c <= '9' {
			return c
		}
		return -1
	}, phone)
	digits = strings.TrimPrefix(digits, "00")
	if len(digits) <= 6 || digits[0] == '0' {
		return "", &Error{"bad_phone", "the phone number needs its country code"}
	}
	return digits, nil
}

func (s *Session) handleCode(req json.RawMessage) (any, error) {
	var r struct{ Phone string }
	json.Unmarshal(req, &r)
	digits, err := pairDigits(r.Phone)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.link == linkReady || s.link == linkLinking {
		s.mu.Unlock()
		return nil, &Error{"linked", "this device is already linked"}
	}
	s.restartLogin()
	if err := s.startQR(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	cli, seen := s.cli, s.qrSeen
	s.mu.Unlock()

	// PairPhone needs the login websocket up, which the first QR code shows.
	select {
	case <-seen:
	case <-time.After(20 * time.Second):
		return nil, &Error{"offline", "WhatsApp didn't answer; check the connection and try again"}
	}
	// The server accepts only "Browser (OS)" names it knows; Windows, as identity's.
	code, err := cli.PairPhone(context.Background(), digits, true, whatsmeow.PairClientChrome, "Chrome (Windows)")
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.code = code
	s.setLink(linkCode)
	return map[string]string{"code": code}, nil
}

// restartLogin drops a running login socket, so a pairing code gets the socket's whole life:
// it closes when the QR codes run out, 160 s after the first, and the linking page starts them
// as it opens. The new socket is on a fresh client, because a cancelled QR channel disconnects
// its client later, from its own goroutine. Going through unlinked lets a page that waits for
// "code" see the new one. Call it with s.mu held, while not linked.
func (s *Session) restartLogin() {
	if s.stopQR == nil {
		return
	}
	s.stopQR()
	s.cli.Disconnect()
	s.cli = s.newClient(s.container.NewDevice())
	s.stopQR, s.qr, s.code, s.conn = nil, "", "", "offline"
	s.setLink(linkUnlinked)
}

func (s *Session) handleLogout(json.RawMessage) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cli.Store.ID != nil {
		if err := s.cli.Logout(context.Background()); err != nil {
			return nil, err
		}
	}
	return nil, s.reset()
}

// reset drops the client and starts over with a fresh device. Call it with s.mu held.
func (s *Session) reset() error {
	if s.stopQR != nil {
		s.stopQR()
	}
	s.cli.Disconnect()
	s.calls.drop()
	s.store.clear()
	s.syncing = false
	s.cli = s.newClient(s.container.NewDevice())
	s.stopQR, s.qr, s.code, s.conn = nil, "", "", "offline"
	s.setLink(linkUnlinked)
	return nil
}

func (s *Session) onEvent(cli *whatsmeow.Client, evt any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cli != s.cli {
		return // an event from a client that reset() already replaced
	}
	switch e := evt.(type) {
	case *events.PairSuccess:
		// A new link starts a new history: nothing from an older link, or from the simulated
		// account (sim.go), stays in chats.db.
		s.store.clear()
		s.syncing = true
		s.setLink(linkLinking)
		time.AfterFunc(syncCap, s.endSync)
	case *events.Connected:
		s.conn = "ready"
		if cli.Store.ID != nil {
			if s.stopQR != nil {
				s.stopQR()
				s.stopQR = nil
			}
			s.setLink(linkReady)
			// The app's focus again, off this goroutine: a send waits on the connection.
			go func() {
				if err := sendPresence(cli, s.store.ui.shown()); err != nil {
					s.log.Warnf("presence after connecting: %v", err)
				}
			}()
		}
		s.srv.Emit("conn", map[string]any{"state": s.conn})
		s.photos.connected()
	case *events.Picture:
		s.photos.changed(cli, e.JID)
	case *events.Disconnected:
		// A call's media runs on its own relay and may outlive a short drop, which hypermeow mends
		// by itself; only a connection still down after callDropGrace ends the call.
		time.AfterFunc(callDropGrace, func() {
			if !cli.IsConnected() {
				s.calls.drop()
			}
		})
		s.conn = "connecting" // hypermeow reconnects by itself
		s.srv.Emit("conn", map[string]any{"state": s.conn})
	case *events.StreamReplaced:
		s.calls.drop()
		s.conn = "offline" // another engine took this session over
		s.srv.Emit("conn", map[string]any{"state": s.conn, "reason": "replaced"})
	case *events.LoggedOut:
		s.log.Warnf("logged out: %v", e.Reason)
		// Off this goroutine: reset disconnects the very client delivering the event.
		go func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if cli == s.cli {
				s.reset()
			}
		}()
	}
}
