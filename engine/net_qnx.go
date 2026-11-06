//go:build qnx

package main

/*
#cgo LDFLAGS: -lbps
#cgo CFLAGS: -Wno-deprecated-declarations
#include <bps/bps.h>
#include <bps/netstatus.h>

// 1 when the default network interface is the cellular one; 0 for anything else, and for any failure.
static int hb_on_cellular(void) {
	char *name = NULL;
	netstatus_interface_details_t *d = NULL;
	int cell = 0;
	if (bps_initialize() != BPS_SUCCESS)
		return 0;
	if (netstatus_get_default_interface(&name) == BPS_SUCCESS && name != NULL) {
		if (netstatus_get_interface_details(name, &d) == BPS_SUCCESS && d != NULL) {
			cell = netstatus_interface_get_type(d) == NETSTATUS_INTERFACE_TYPE_CELLULAR;
			netstatus_free_interface_details(&d);
		}
		bps_free(name);
	}
	bps_shutdown();
	return cell;
}
*/
import "C"

import "runtime"

// cellular is whether the default network interface is mobile data. BPS is per thread, hence the
// lock. Any failure reads as not cellular, so nothing stops a download by accident.
func cellular() bool {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return C.hb_on_cellular() != 0
}
