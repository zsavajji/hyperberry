//go:build qnx

package main

/*
#cgo LDFLAGS: -lbps
#include <errno.h>
#include <stdbool.h> // bps/led.h uses bool without it
#include <stdlib.h>
#include <string.h>
#include <bps/bps.h>
#include <bps/led.h>
#include <bps/notification.h>

// Opens BPS on the calling thread for good: an LED request may end with the session that made
// it, so the notifier's thread keeps one open (step 5.6).
static int hb_bps_open(void) {
	return bps_initialize() == BPS_SUCCESS ? 0 : errno;
}

// Flashes the LED in rgb until cancelled; rgb 0 cancels.
static int hb_led(const char *id, int rgb) {
	int rc = rgb ? led_request_rgb(id, rgb, 0) : led_cancel(id);
	return rc == BPS_SUCCESS ? 0 : errno;
}

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

// ledOpen keeps BPS open on the calling thread, which must stay locked to its goroutine.
func ledOpen() error { return errnoErr(C.hb_bps_open()) }

// led flashes the LED in rgb (0x00rrggbb; any non-zero part is full, so 7 colours) until
// led(id, 0). Needs access_led_control, and the thread ledOpen opened.
func led(id string, rgb int) error {
	cid := C.CString(id)
	defer C.free(unsafe.Pointer(cid))
	return errnoErr(C.hb_led(cid, C.int(rgb)))
}

func errnoErr(rc C.int) error {
	if rc == 0 {
		return nil
	}
	return syscall.Errno(rc)
}
