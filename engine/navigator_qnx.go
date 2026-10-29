//go:build qnx

package main

/*
#cgo LDFLAGS: -lbps
#include <bps/bps.h>
#include <bps/navigator.h>

// 1 when the navigator asks the app to exit, 0 for any other event, -1 on error.
static int hb_next_event(void) {
	bps_event_t *ev = NULL;
	if (bps_get_event(&ev, -1) != BPS_SUCCESS)
		return -1;
	return ev && bps_event_get_domain(ev) == navigator_get_domain() &&
		bps_event_get_code(ev) == NAVIGATOR_EXIT;
}
*/
import "C"

import (
	"os"
	"runtime"
	"syscall"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// connectNavigator tells the navigator the engine is up: it slays a headless app that hasn't
// connected within 6 s of launch (Q10, 2026-10-29). NAVIGATOR_EXIT, sent before a reinstall
// with 3 s to go, becomes a SIGTERM on stop. Started from a shell there's no navigator to
// connect to, and the engine runs on without it.
func connectNavigator(stop chan<- os.Signal, log waLog.Logger) {
	go func() {
		// BPS is per thread: the init, the request and every bps_get_event on this one.
		runtime.LockOSThread()
		if C.bps_initialize() != C.BPS_SUCCESS || C.navigator_request_events(0) != C.BPS_SUCCESS {
			log.Warnf("not connected to the navigator")
			return
		}
		for {
			switch C.hb_next_event() {
			case 1:
				stop <- syscall.SIGTERM
				return
			case -1:
				log.Errorf("bps_get_event failed")
				return
			}
		}
	}()
}
