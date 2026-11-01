//go:build qnx

package main

/*
#cgo LDFLAGS: -lbps
#include <bps/bps.h>
#include <bps/navigator.h>
#include <bps/navigator_invoke.h>

// 1 when the navigator asks the app to exit; 2 for an invoke, its action, MIME type, URI and
// data in the out parameters until the next call; 0 for any other event; -1 on error.
static int hb_next_event(const char **action, const char **type, const char **uri,
		const void **data, int *len) {
	bps_event_t *ev = NULL;
	const navigator_invoke_invocation_t *inv;
	if (bps_get_event(&ev, -1) != BPS_SUCCESS)
		return -1;
	if (!ev || bps_event_get_domain(ev) != navigator_get_domain())
		return 0;
	switch (bps_event_get_code(ev)) {
	case NAVIGATOR_EXIT:
		return 1;
	case NAVIGATOR_INVOKE_TARGET:
		inv = navigator_invoke_event_get_invocation(ev);
		*action = navigator_invoke_invocation_get_action(inv);
		*type = navigator_invoke_invocation_get_type(inv);
		*uri = navigator_invoke_invocation_get_uri(inv);
		*data = navigator_invoke_invocation_get_data(inv);
		*len = navigator_invoke_invocation_get_data_length(inv);
		return *action ? 2 : 0;
	}
	return 0;
}
*/
import "C"

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// connectNavigator tells the navigator the engine is up: it slays a headless app that hasn't
// connected within 6 s of launch (Q10, 2026-10-29). NAVIGATOR_EXIT, sent before a reinstall
// with 3 s to go, becomes a SIGTERM on stop. Each invoke goes to invoked, copied into Go
// memory; invoked runs on the BPS thread and must not block. Started from a shell there's no
// navigator to connect to, and the engine runs on without it.
func connectNavigator(stop chan<- os.Signal, invoked func(invocation), log waLog.Logger) {
	go func() {
		// BPS is per thread: the init, the request and every bps_get_event on this one.
		runtime.LockOSThread()
		if C.bps_initialize() != C.BPS_SUCCESS || C.navigator_request_events(0) != C.BPS_SUCCESS {
			log.Warnf("not connected to the navigator")
			return
		}
		for {
			var action, typ, uri *C.char
			var data unsafe.Pointer
			var n C.int
			switch C.hb_next_event(&action, &typ, &uri, &data, &n) {
			case 1:
				stop <- syscall.SIGTERM
				return
			case 2:
				// The C strings and data are the next event's to free: copy them now.
				invoked(invocation{C.GoString(action), C.GoString(typ), C.GoString(uri), C.GoBytes(data, n)})
			case -1:
				log.Errorf("bps_get_event failed")
				return
			}
		}
	}()
}
