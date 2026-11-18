// hyperberryd is the WhatsApp engine of HyperBerry (PLAN.md §6). It serves the UI over
// loopback TCP.

// Names through libsocket's getaddrinfo: BB10 keeps each interface's DNS servers in
// /pps/services/networking/interfaces, and on mobile data _CS_RESOLVE (all Go's resolver
// reads) holds msm1's placeholder 0.0.0.0, so no lookup worked (a user's log, 2026-11-18).
//
//go:debug netdns=cgo
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	rdebug "runtime/debug"

	_ "github.com/mattn/go-sqlite3"
	"github.com/polymorfa/hypermeow/proto/waCompanionReg"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/store/sqlstore"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"google.golang.org/protobuf/proto"
)

func main() {
	// The phone caps the engine at about 30 MB (phase6.md 6.3). Near this limit the collector
	// works harder and returns memory to the OS sooner, instead of the heap growing till the OS
	// kills us.
	// ponytail: the real account's live heap is unmeasured; if it lives above this the collector
	// will run often (raise the limit, or measure the heap).
	rdebug.SetMemoryLimit(24 << 20)

	// Started as the headless service the engine gets no arguments; BB10 sets HOME to the
	// app's data/, which the UI reads too.
	home := os.Getenv("HOME")
	if home == "" {
		home = "."
	}
	data := flag.String("data", home, "data directory: session.db, chats.db, engine.json, logs/")
	logTo := flag.String("log", "", `log file (default <data>/logs/hyperberryd.log; "-" for stderr)`)
	debug := flag.Bool("debug", false, "log the protocol (as does a file <data>/debug)")
	osName := flag.String("os", "Windows", "OS name the phone shows for this device (fixed at linking)")
	days := flag.Uint("days", 90, "ask the phone for this many days of history at linking (0: all)")
	sim := flag.Bool("sim", false, "serve the simulated account of step 1.12 (as does a file <data>/sim)")
	flag.Parse()

	level := "INFO"
	// -debug, or a file <data>/debug: the phone starts the engine with no arguments, so a marker file is how it
	// is turned on there (delete the file and restart to turn it off).
	if _, err := os.Stat(filepath.Join(*data, "debug")); *debug || err == nil {
		level = "DEBUG"
	}
	var log waLog.Logger
	if *logTo == "-" {
		log = newLogger(os.Stderr, level)
	} else {
		path := *logTo
		if path == "" {
			path = filepath.Join(*data, "logs", "hyperberryd.log")
		}
		w, err := openRotator(path, logMax)
		if err != nil {
			fatal("log:", err)
		}
		fatalOut = w
		log = newLogger(w, level)
		// A panic or a fatal error (out of memory among them) goes to stderr, which nothing
		// reads on the phone: crash.log keeps a copy. A kill from outside leaves nothing there.
		if f, err := os.OpenFile(filepath.Join(filepath.Dir(path), "crash.log"),
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			if err := rdebug.SetCrashOutput(f, rdebug.CrashOptions{}); err != nil {
				log.Warnf("crash output: %v", err)
			}
		}
	}

	log.Infof("engine memory at start: %s", memUsage())

	identity(*osName, *days)
	ctx := context.Background()
	// WAL, as chats.db: in the rollback journal a read held over 5 s on one of hypermeow's
	// connections failed another's write ("database is locked", the link's history, 2026-11-01).
	// Immediate transactions, as WAL fails a read-then-write one (decryption) at once if another
	// write landed between.
	db := "file:" + filepath.Join(*data, "session.db") + "?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate"
	container, err := sqlstore.New(ctx, "sqlite3", db, log.Sub("db"))
	if err != nil {
		fatal("open store:", err)
	}
	if _, err := os.Stat(filepath.Join(*data, "sim")); err == nil {
		*sim = true
	}
	runEngine(container, *data, log, *sim)
}

// identity sets how this device presents itself when it links; the phone keeps it from then on.
// A baileys client found (2026-09) that a web identity gets only the latest few chats, and
// Windows as Desktop with full sync gets the whole history; macOS as Desktop was refused.
// The version is Windows 11 23H2's, as WhatsApp for Windows reports it; the library's 0.1.0 marks it.
func identity(osName string, days uint) {
	store.SetOSInfo(osName, [3]uint32{10, 0, 22631})
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
	store.DeviceProps.RequireFullSync = proto.Bool(true)
	if days > 0 {
		store.DeviceProps.HistorySyncConfig.FullSyncDaysLimit = proto.Uint32(uint32(days))
	}
}

// fatalOut is where fatal says why: stderr, which nothing reads on the phone, until main has the log.
var fatalOut io.Writer = os.Stderr

func fatal(v ...any) {
	fmt.Fprintln(fatalOut, v...)
	os.Exit(1)
}
