//go:build qnx

package main

/*
#cgo LDFLAGS: -lmmrndclient -lstrm
#include <stdlib.h>
#include <sys/stat.h>
#include <mm/renderer.h>
#include <mm/renderer/events.h>

// One ringtone: the connection and the context it plays in.
typedef struct {
	mmr_connection_t *conn;
	mmr_context_t *ctx;
} hb_ring;

// hb_ring_start plays url as a ringtone, repeating, through mm-renderer (the system's player
// service, so the engine holds no audio). 0 on success, else the step that failed; hb_ring_stop
// cleans up after either.
static int hb_ring_start(hb_ring *r, const char *url) {
	strm_dict_t *d;
	int out;
	if (!(r->conn = mmr_connect(NULL)))
		return 1;
	if (!(r->ctx = mmr_context_create(r->conn, "hyperberry_ring", 0, S_IRWXU)))
		return 2;
	if ((out = mmr_output_attach(r->ctx, "audio:default", "audio")) < 0)
		return 3;
	// The dictionaries become the API's, even when a call fails.
	if (!(d = strm_dict_new()) || !(d = strm_dict_set(d, "audio_type", "ringtone")))
		return 4;
	if (mmr_output_parameters(r->ctx, out, d) != 0)
		return 5;
	if (mmr_input_attach(r->ctx, url, "track") != 0)
		return 6;
	if (!(d = strm_dict_new()) || !(d = strm_dict_set(d, "repeat", "track")))
		return 7;
	if (mmr_input_parameters(r->ctx, d) != 0)
		return 8;
	if (mmr_play(r->ctx) != 0)
		return 9;
	return 0;
}

// hb_ring_again starts the tone over once mm-renderer has stopped at its end: on the Q10 the
// repeat parameter didn't loop it (0.0.1.185 rang once). It drains the pending events; the last,
// MMR_EVENT_NONE, still carries the context's state. 1 when it started over.
static int hb_ring_again(hb_ring *r) {
	const mmr_event_t *ev;
	do
		ev = mmr_event_get(r->ctx);
	while (ev && ev->type != MMR_EVENT_NONE);
	if (!ev || ev->state != MMR_STATE_STOPPED)
		return 0;
	mmr_seek(r->ctx, "0");
	return mmr_play(r->ctx) == 0;
}

static void hb_ring_stop(hb_ring *r) {
	if (r->ctx) {
		mmr_stop(r->ctx);
		mmr_context_destroy(r->ctx);
	}
	if (r->conn)
		mmr_disconnect(r->conn);
	free(r);
}
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"
)

// startRing plays file as a ringtone, over and over, until stop is called. The caller runs it on a
// goroutine of its own, so nothing on the call's path waits on mm-renderer.
func startRing(file string) (stop func(), err error) {
	url := C.CString("file://" + file)
	defer C.free(unsafe.Pointer(url))
	r := (*C.hb_ring)(C.calloc(1, C.size_t(unsafe.Sizeof(C.hb_ring{}))))
	if rc := C.hb_ring_start(r, url); rc != 0 {
		C.hb_ring_stop(r)
		return nil, fmt.Errorf("mm-renderer failed at step %d", int(rc))
	}
	// Every mm-renderer call after the start is on this goroutine: a look every half second
	// whether the tone has ended, to start it over, until stop.
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		defer C.hb_ring_stop(r)
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		var last time.Time
		quick := 0
		for {
			select {
			case <-quit:
				return
			case <-tick.C:
				if C.hb_ring_again(r) == 0 {
					continue
				}
				// A tone that stops again at once is failing, not ending: three times and it's left off.
				if time.Since(last) < 2*time.Second {
					if quick++; quick == 3 {
						<-quit
						return
					}
				} else {
					quick = 0
				}
				last = time.Now()
			}
		}
	}()
	return func() { close(quit); <-done }, nil
}
