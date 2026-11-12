package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/polymorfa/hypermeow/store/sqlstore"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// runEngine serves the session to the UI until a signal; with sim, the simulated account
// (sim.go) instead, unless this engine is linked.
func runEngine(container *sqlstore.Container, dataDir string, log waLog.Logger, sim bool) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	// First, before anything slow: the navigator gives a headless app 6 s.
	// The launch itself is the start; a START or a boot timer's fire sent to a running engine needs nothing. The
	// Hub's actions wait in invokes for hubActions; the callback runs on the BPS thread, so it
	// drops what doesn't fit.
	invokes := make(chan invocation, 16)
	// The same way for the network: a change wakes one call to NetworkChanged; nw is only touched
	// on the BPS thread.
	netEvents := make(chan struct{}, 1)
	var nw netWatch
	connectNavigator(stop, func(inv invocation) {
		select {
		case invokes <- inv:
		default:
		}
	}, func(key string) {
		if nw.moved(key) {
			select {
			case netEvents <- struct{}{}:
			default:
			}
		}
	}, log.Sub("nav"))
	if sim {
		// Simulated rows must never mix with a real history.
		if d, err := container.GetFirstDevice(context.Background()); err != nil || d.ID != nil {
			log.Warnf("not simulating: this engine is linked (or session.db failed: %v)", err)
			sim = false
		}
	}
	var hello func() any
	db, err := openChats(filepath.Join(dataDir, "chats.db"))
	if err != nil {
		fatal("chats.db:", err)
	}
	defer db.Close()
	srv, err := NewServer(dataDir, log.Sub("server"), func() any { return hello() })
	if err != nil {
		fatal("server:", err)
	}
	phoneBook.load(filepath.Join(dataDir, "book.json")) // before the store names anyone
	st := newStore(db, srv, log.Sub("store"))
	closeSession := func() {}
	if sim {
		log.Infof("simulating an account (step 1.12): nothing goes to WhatsApp")
		hello = newSim(srv, st, log.Sub("sim")).hello
		registerSimCalls(srv)
	} else {
		sess, err := newSession(container, srv, st, log.Sub("session"))
		if err != nil {
			srv.Close()
			st.close()
			fatal("session:", err)
		}
		hello, closeSession = sess.hello, sess.close
		go func() {
			for range netEvents {
				sess.networkChanged()
			}
		}()
	}
	go hubActions(srv, invokes, st.nt.removeItem, log.Sub("hub"))
	go func() {
		if err := srv.Serve(); err != nil {
			log.Errorf("server: %v", err)
		}
	}()
	go pruneCache(dataDir, st.nt.cacheLimit(), "", time.Now(), log)
	log.Infof("serving the UI")
	go logStats(log)
	<-stop
	srv.Close()
	closeSession()
	st.close()
}
