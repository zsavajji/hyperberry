//go:build qnx

package main

/*
#cgo LDFLAGS: -lbps
#cgo CFLAGS: -Wno-deprecated-declarations
#include <stdio.h>
#include <bps/bps.h>
#include <bps/navigator.h>
#include <bps/navigator_invoke.h>
#include <bps/netstatus.h>

// 1 when the navigator asks the app to exit; 2 for an invoke, its action, MIME type, URI and
// data in the out parameters until the next call; 3 for a network status event, net holding
// "default interface|first default gateway" ("" with no network); 0 for any other event; -1 on error.
static int hb_next_event(const char **action, const char **type, const char **uri,
		const void **data, int *len, char *net, int netlen) {
	bps_event_t *ev = NULL;
	const navigator_invoke_invocation_t *inv;
	netstatus_info_t *info;
	const char *ifc, *gw;
	if (bps_get_event(&ev, -1) != BPS_SUCCESS)
		return -1;
	if (ev && bps_event_get_domain(ev) == netstatus_get_domain()) {
		// The event owns info: no free.
		if (bps_event_get_code(ev) != NETSTATUS_INFO || !(info = netstatus_event_get_info(ev)))
			return 0;
		net[0] = 0;
		if (netstatus_info_get_availability(info)) {
			ifc = netstatus_info_get_default_interface(info);
			gw = netstatus_info_get_num_default_gateways(info) > 0 ?
				netstatus_info_get_default_gateway(info, 0) : NULL;
			snprintf(net, netlen, "%s|%s", ifc ? ifc : "", gw ? gw : "");
		}
		return 3;
	}
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
// memory; invoked runs on the BPS thread and must not block. Each network status event goes to
// netChanged as a key of the default interface and its gateway ("" with no network); it runs on
// the BPS thread too. Started from a shell there's no navigator to connect to, and the engine
// runs on without it.
func connectNavigator(stop chan<- os.Signal, invoked func(invocation), netChanged func(key string), log waLog.Logger) {
	go func() {
		// BPS is per thread: the init, the request and every bps_get_event on this one.
		runtime.LockOSThread()
		if C.bps_initialize() != C.BPS_SUCCESS || C.navigator_request_events(0) != C.BPS_SUCCESS {
			log.Warnf("not connected to the navigator")
			return
		}
		if C.netstatus_request_events(0) != C.BPS_SUCCESS {
			log.Warnf("no network status events")
		}
		for {
			var action, typ, uri *C.char
			var data unsafe.Pointer
			var n C.int
			var net [128]C.char
			switch C.hb_next_event(&action, &typ, &uri, &data, &n, &net[0], C.int(len(net))) {
			case 1:
				stop <- syscall.SIGTERM
				return
			case 2:
				// The C strings and data are the next event's to free: copy them now.
				invoked(invocation{C.GoString(action), C.GoString(typ), C.GoString(uri), C.GoBytes(data, n)})
			case 3:
				netChanged(C.GoString(&net[0]))
			case -1:
				log.Errorf("bps_get_event failed")
				return
			}
		}
	}()
}
