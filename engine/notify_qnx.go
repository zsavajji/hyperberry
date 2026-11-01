//go:build qnx

package main

/*
#cgo LDFLAGS: -lbps
#include <errno.h>
#include <stdlib.h>
#include <string.h>
#include <bps/bps.h>
#include <bps/notification.h>

// Each call initialises BPS on the calling thread, so any goroutine can post. 0 on success,
// otherwise errno.
static int hb_notification(const char *item, const char *title, const char *body,
		const char *payload) {
	notification_message_t *msg = NULL;
	int rc;
	if (bps_initialize() != BPS_SUCCESS)
		return errno;
	if (notification_message_create(&msg) != BPS_SUCCESS) {
		rc = errno;
		bps_shutdown();
		return rc;
	}
	notification_message_set_item_id(msg, item);
	// Posting an item id that's still live fails, so a newer notification replaces it. Only a
	// withdraw (no title) reports a failed delete: a post may have nothing to replace.
	rc = notification_delete(msg) == BPS_SUCCESS || title ? 0 : errno;
	if (title) {
		notification_message_set_title(msg, title);
		notification_message_set_subtitle(msg, body);
		notification_message_set_invocation_target(msg, "com.zsavajji.hyperberry");
		notification_message_set_invocation_action(msg, "com.zsavajji.hyperberry.OPEN");
		notification_message_set_invocation_type(msg, "text/plain");
		notification_message_set_invocation_payload(msg, payload, strlen(payload));
		rc = notification_notify(msg) == BPS_SUCCESS ? 0 : errno;
	}
	notification_message_destroy(&msg);
	bps_shutdown();
	return rc;
}
*/
import "C"

import (
	"syscall"
	"unsafe"
)

// notify posts the notification itemID to the Hub, replacing one still there, with the LED
// and sound the user set for the app. Tapping it opens the UI with action
// com.zsavajji.hyperberry.OPEN and payload as text/plain.
func notify(itemID, title, body, payload string) error {
	cs := []*C.char{C.CString(itemID), C.CString(title), C.CString(body), C.CString(payload)}
	defer func() {
		for _, s := range cs {
			C.free(unsafe.Pointer(s))
		}
	}()
	return errnoErr(C.hb_notification(cs[0], cs[1], cs[2], cs[3]))
}

// withdraw removes the notification itemID from the Hub.
func withdraw(itemID string) error {
	item := C.CString(itemID)
	defer C.free(unsafe.Pointer(item))
	return errnoErr(C.hb_notification(item, nil, nil, nil))
}

func errnoErr(rc C.int) error {
	if rc == 0 {
		return nil
	}
	return syscall.Errno(rc)
}
