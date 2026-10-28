// hyperberryd is the WhatsApp engine of HyperBerry (PLAN.md §6). It serves the UI over
// loopback TCP; with -probe it is the terminal probe of step 0.2 instead.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"github.com/polymorfa/hypermeow/proto/waCompanionReg"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/store/sqlstore"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"google.golang.org/protobuf/proto"
)

func main() {
	// Started as the headless service the engine gets no arguments; BB10 sets HOME to the
	// app's data/, which the UI reads too.
	home := os.Getenv("HOME")
	if home == "" {
		home = "."
	}
	data := flag.String("data", home, "data directory: session.db, chats.db, engine.json, logs/")
	logTo := flag.String("log", "", `log file (default <data>/logs/hyperberryd.log; "-" for stderr)`)
	debug := flag.Bool("debug", false, "log the protocol")
	osName := flag.String("os", "Windows", "OS name the phone shows for this device (fixed at linking)")
	days := flag.Uint("days", 90, "ask the phone for this many days of history at linking (0: all)")
	probe := flag.Bool("probe", false, "run the terminal probe of step 0.2 instead of serving the UI")
	phone := flag.String("pair", "", "probe: link to this phone number by pairing code (with country code)")
	qrMode := flag.Bool("qr", false, "probe: link by QR code, printing each code for the main phone to scan")
	flag.Parse()

	level := "INFO"
	if *debug {
		level = "DEBUG"
	}
	var log waLog.Logger
	switch {
	case *probe:
		log = waLog.Stdout("wa", level, false)
	case *logTo == "-":
		log = newLogger(os.Stderr, level)
	default:
		path := *logTo
		if path == "" {
			path = filepath.Join(*data, "logs", "hyperberryd.log")
		}
		w, err := openRotator(path, logMax)
		if err != nil {
			fatal("log:", err)
		}
		log = newLogger(w, level)
	}

	identity(*osName, *days)
	ctx := context.Background()
	db := "file:" + filepath.Join(*data, "session.db") + "?_foreign_keys=on"
	container, err := sqlstore.New(ctx, "sqlite3", db, log.Sub("db"))
	if err != nil {
		fatal("open store:", err)
	}
	if *probe {
		runProbe(ctx, container, log, *phone, *qrMode)
		return
	}
	runEngine(container, *data, log)
}

// identity sets how this device presents itself when it links; the phone keeps it from then on.
// A baileys client found (2026-09) that a web identity gets only the latest few chats, and
// Windows as Desktop with full sync gets the whole history; macOS as Desktop was refused.
func identity(osName string, days uint) {
	store.SetOSInfo(osName, [3]uint32{0, 1, 0})
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
	store.DeviceProps.RequireFullSync = proto.Bool(true)
	if days > 0 {
		store.DeviceProps.HistorySyncConfig.FullSyncDaysLimit = proto.Uint32(uint32(days))
	}
}

func fatal(v ...any) {
	fmt.Fprintln(os.Stderr, v...)
	os.Exit(1)
}
