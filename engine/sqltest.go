package main

import (
	"time"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// sqlTest is step 0.4's writer (PLAN.md §9): a row every 50 ms for 6 min into sqltest.db,
// opened like chats.db, while the UI's test page reads the newest row from its own process.
// A file of its own, because chats.db is the only copy of the history. It goes when Phase 2
// replaces the gate app.
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
