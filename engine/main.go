// hyperberryd is the WhatsApp engine of HyperBerry. This is its Phase 0 form, a probe run from a
// terminal on the phone: it links by pairing code, prints what arrives, and takes a few
// commands on stdin (see PLAN.md, task 0.2).
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	_ "github.com/mattn/go-sqlite3"
	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waCompanionReg"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/store/sqlstore"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"google.golang.org/protobuf/proto"
)

func main() {
	data := flag.String("data", ".", "directory for session.db")
	phone := flag.String("pair", "", "link to this phone number by pairing code (with country code)")
	osName := flag.String("os", "Windows", "OS name the phone shows for this device (fixed at pairing)")
	days := flag.Uint("days", 90, "ask the phone for this many days of history at pairing (0: all)")
	debug := flag.Bool("debug", false, "log the protocol")
	flag.Parse()

	level := "INFO"
	if *debug {
		level = "DEBUG"
	}
	log := waLog.Stdout("wa", level, false)
	ctx := context.Background()

	// sup (baileys, 2026-09) found that a web identity gets only the latest few chats, and
	// Windows as Desktop with full sync gets the whole history; macOS as Desktop was refused.
	store.SetOSInfo(*osName, [3]uint32{0, 1, 0})
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
	store.DeviceProps.RequireFullSync = proto.Bool(true)
	if *days > 0 {
		store.DeviceProps.HistorySyncConfig.FullSyncDaysLimit = proto.Uint32(uint32(*days))
	}
	db := "file:" + filepath.Join(*data, "session.db") + "?_foreign_keys=on"
	container, err := sqlstore.New(ctx, "sqlite3", db, log.Sub("db"))
	if err != nil {
		fatal("open store:", err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		fatal("load device:", err)
	}
	cli := whatsmeow.NewClient(device, log.Sub("client"))
	cli.AddEventHandler(func(evt any) { onEvent(cli, evt) })

	if cli.Store.ID == nil {
		if *phone == "" {
			fatal("not linked yet: run with -pair <number>, e.g. -pair 393331234567")
		}
		pair(ctx, cli, *phone)
	} else if err := cli.Connect(); err != nil {
		fatal("connect:", err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		cli.Disconnect()
		os.Exit(0)
	}()
	commands(ctx, cli)
}

// pair connects, waits for the first QR code (the websocket is then ready) and asks for a
// pairing code instead. The login socket closes when the QR codes run out, about 160 s.
func pair(ctx context.Context, cli *whatsmeow.Client, phone string) {
	qr, err := cli.GetQRChannel(ctx)
	if err != nil {
		fatal("qr channel:", err)
	}
	if err := cli.Connect(); err != nil {
		fatal("connect:", err)
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	go func() {
		asked := false
		for item := range qr {
			switch {
			case item.Event == whatsmeow.QRChannelEventCode && !asked:
				asked = true
				// The server accepts only "Browser (OS)" names it knows.
				code, err := cli.PairPhone(ctx, digits, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
				if err != nil {
					fatal("pair:", err)
				}
				fmt.Println("PAIRING CODE:", code)
				fmt.Println("On the phone: WhatsApp > Linked devices > Link a device > Link with phone number instead")
			case item.Event == whatsmeow.QRChannelEventCode:
			case item.Event == whatsmeow.QRChannelEventError:
				fmt.Println("pairing failed:", item.Error)
			default:
				fmt.Println("pairing:", item.Event)
			}
		}
	}()
}

func onEvent(cli *whatsmeow.Client, evt any) {
	switch e := evt.(type) {
	case *events.PairSuccess:
		fmt.Println("linked as", e.ID, "platform", e.Platform)
	case *events.Connected:
		fmt.Println("connected")
		mem()
	case *events.Disconnected:
		fmt.Println("disconnected")
	case *events.LoggedOut:
		fmt.Println("logged out:", e.Reason)
		os.Exit(1)
	case *events.StreamReplaced:
		fmt.Println("another hyperberryd took over this session")
		os.Exit(1)
	case *events.OfflineSyncCompleted:
		fmt.Println("offline sync done:", e.Count, "events")
	case *events.HistorySync:
		convs := e.Data.GetConversations()
		msgs := 0
		for _, c := range convs {
			msgs += len(c.GetMessages())
		}
		fmt.Printf("history %s: %d chats, %d messages, %d push names\n",
			e.Data.GetSyncType(), len(convs), msgs, len(e.Data.GetPushnames()))
		mem()
	case *events.Message:
		who := e.Info.PushName
		if e.Info.IsFromMe {
			who = "me"
		}
		fmt.Printf("%s %s | %s (%s): %s\n", e.Info.Timestamp.Format("15:04"), e.Info.Chat,
			who, e.Info.Sender.User, text(e.Message))
	case *events.Receipt:
		fmt.Println("receipt", e.Type, e.Chat, len(e.MessageIDs), "messages")
	}
}

func text(m *waE2E.Message) string {
	switch {
	case m.GetConversation() != "":
		return m.GetConversation()
	case m.GetExtendedTextMessage() != nil:
		return m.GetExtendedTextMessage().GetText()
	case m.GetImageMessage() != nil:
		return "[photo] " + m.GetImageMessage().GetCaption()
	case m.GetVideoMessage() != nil:
		return "[video] " + m.GetVideoMessage().GetCaption()
	case m.GetAudioMessage() != nil:
		return "[audio]"
	case m.GetDocumentMessage() != nil:
		return "[file] " + m.GetDocumentMessage().GetFileName()
	case m.GetStickerMessage() != nil:
		return "[sticker]"
	case m.GetReactionMessage() != nil:
		return "[reaction] " + m.GetReactionMessage().GetText()
	}
	return "[other]"
}

func mem() {
	var s runtime.MemStats
	runtime.ReadMemStats(&s)
	fmt.Printf("mem: %d MB from the OS, %d MB heap in use, %d GCs\n", s.Sys>>20, s.HeapInuse>>20, s.NumGC)
}

// commands reads stdin until "quit". Without a terminal (stdin at EOF) it keeps running.
func commands(ctx context.Context, cli *whatsmeow.Client) {
	help := "commands: send <number|jid> <text> · groups · mem · quit"
	fmt.Println(help)
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		f := strings.Fields(in.Text())
		switch {
		case len(f) == 0:
		case f[0] == "quit":
			cli.Disconnect()
			return
		case f[0] == "mem":
			mem()
		case f[0] == "groups":
			groups, err := cli.GetJoinedGroups(ctx)
			if err != nil {
				fmt.Println("groups:", err)
				continue
			}
			for _, g := range groups {
				fmt.Printf("%s  %s (%d members)\n", g.JID, g.Name, len(g.Participants))
			}
		case f[0] == "send" && len(f) >= 3:
			to, err := target(f[1])
			if err != nil {
				fmt.Println("send:", err)
				continue
			}
			body := strings.Join(f[2:], " ")
			resp, err := cli.SendMessage(ctx, to, &waE2E.Message{Conversation: proto.String(body)})
			if err != nil {
				fmt.Println("send:", err)
				continue
			}
			fmt.Println("sent", resp.ID, "at", resp.Timestamp.Format("15:04:05"))
		default:
			fmt.Println(help)
		}
	}
	select {}
}

func target(s string) (types.JID, error) {
	if strings.Contains(s, "@") {
		return types.ParseJID(s)
	}
	return types.NewJID(strings.TrimPrefix(s, "+"), types.DefaultUserServer), nil
}

func fatal(v ...any) {
	fmt.Fprintln(os.Stderr, v...)
	os.Exit(1)
}
