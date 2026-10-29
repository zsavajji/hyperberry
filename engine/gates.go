package main

import (
	"path/filepath"
	"time"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// gateInvoked runs the Phase 0 gate checks (PLAN.md §9) that the UI's gate page asks for by
// invoking the engine's target. All of it goes when Phase 2 replaces the gate app.
func gateInvoked(action, dataDir string, log waLog.Logger) {
	switch action {
	case "com.zsavajji.hyperberry.SQLTEST":
		go sqlTest(filepath.Join(dataDir, "sqltest.db"), log.Sub("sqltest"))
	case "com.zsavajji.hyperberry.NOTIFYTEST":
		go func() {
			// Time to lock the phone first: posted while the phone was in use, the LED was
			// cancelled by the system 2 s later and never blinked (Q10, 2026-10-29).
			time.Sleep(10 * time.Second)
			body := "Notification test, " + time.Now().Format("15:04:05")
			if err := notify("test", "HyperBerry", body, "test-chat"); err != nil {
				log.Errorf("notify: %v", err)
			} else {
				log.Infof("notification posted")
			}
		}()
	case "com.zsavajji.hyperberry.WITHDRAWTEST":
		go func() {
			if err := withdraw("test"); err != nil {
				log.Errorf("withdraw: %v", err)
			} else {
				log.Infof("notification withdrawn")
			}
		}()
	}
}

// sqlTest is step 0.4's writer: a row every 50 ms for 6 min into sqltest.db, opened like
// chats.db, while the UI's test page reads the newest row from its own process. A file of its
// own, because chats.db is the only copy of the history.
func sqlTest(path string, log waLog.Logger) {
	db, err := openChats(path)
	if err != nil {
		log.Errorf("open: %v", err)
		return
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS sqltest(n INTEGER PRIMARY KEY, at INTEGER NOT NULL)`); err != nil {
		log.Errorf("create: %v", err)
		return
	}
	const rows = 6 * 60 * 20
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	failed := 0
	for i := 0; i < rows; i++ {
		<-tick.C
		if _, err := db.Exec(`INSERT INTO sqltest(at) VALUES(?)`, time.Now().UnixMilli()); err != nil {
			failed++
			log.Warnf("insert: %v", err)
		}
	}
	log.Infof("%d rows written, %d failed", rows-failed, failed)
}
