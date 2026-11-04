package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/polymorfa/hypermeow/socket"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

// After a WhatsApp stream end, a 4-byte message starting 88 02 03 reads as an 8.9 MB frame, and
// allocating that killed the engine (Q10, 2026-11-03). framesocket.patch grows a frame as its
// bytes arrive. This also fails if a build path stops applying the patch.
func TestFrameSocketAllocatesWhatArrives(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		// One frame split over two messages, another sharing the second message, then the claim.
		for _, m := range [][]byte{{0, 0, 5, 'h', 'e'}, {'l', 'l', 'o', 0, 0, 2, 'o', 'k'}, {0x88, 0x02, 0x03, 0xe9}} {
			if err := c.Write(r.Context(), websocket.MessageBinary, m); err != nil {
				return
			}
		}
		c.Close(websocket.StatusGoingAway, "")
	}))
	defer srv.Close()

	fs := socket.NewFrameSocket(waLog.Noop, srv.Client())
	fs.URL = "ws" + strings.TrimPrefix(srv.URL, "http")
	closed := make(chan struct{})
	fs.SetOnDisconnect(func(context.Context, bool) { close(closed) })

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if err := fs.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hello", "ok"} {
		select {
		case got := <-fs.Frames:
			if string(got) != want {
				t.Fatalf("frame %q, want %q", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no frame %q", want)
		}
	}
	// The socket closing is what starts hypermeow's reconnect.
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the socket never reported its close")
	}
	runtime.ReadMemStats(&after)
	if n := after.TotalAlloc - before.TotalAlloc; n > 4<<20 {
		t.Fatalf("allocated %d bytes for 15 bytes of data", n)
	}
}
