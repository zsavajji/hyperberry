package main

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/polymorfa/hypermeow/store/sqlstore"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// runEngine is hyperberryd's normal mode: the session, served to the UI until a signal.
func runEngine(container *sqlstore.Container, dataDir string, log waLog.Logger) {
	var sess *Session
	db, err := openChats(filepath.Join(dataDir, "chats.db"))
	if err != nil {
		fatal("chats.db:", err)
	}
	defer db.Close()
	srv, err := NewServer(dataDir, log.Sub("server"), func() any { return sess.hello() })
	if err != nil {
		fatal("server:", err)
	}
	st := newStore(db, srv, log.Sub("store"))
	if sess, err = newSession(container, srv, st, log.Sub("session")); err != nil {
		srv.Close()
		st.close()
		fatal("session:", err)
	}
	go func() {
		if err := srv.Serve(); err != nil {
			log.Errorf("server: %v", err)
		}
	}()
	log.Infof("serving the UI")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	srv.Close()
	sess.close()
	st.close()
}
