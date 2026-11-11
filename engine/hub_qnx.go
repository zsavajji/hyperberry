//go:build qnx

package main

/*
#cgo LDFLAGS: -lunifieddatasourcec -lbps
#include <stdbool.h>
#include <stdlib.h>
#include <string.h>
#include <bps/bps.h>
#include <bps/locale.h>
#include <bb/pim/unified/unified_data_source.h>

#define HB_MIME "application/vnd.zsavajji.hyperberry.chat"

// The connection to the Hub. Every hb_uds_* call is synchronous and comes from the notifier's
// one thread.
static uds_context_t hb_uds;

// hb_uds_open registers with the Hub (once; a later call finds the connection up). 0 on
// success, with *fresh set when the Hub had never seen this app: it then holds nothing of ours.
static int hb_uds_open(const char *assets, int *fresh) {
	int rc;
	if (!hb_uds && (rc = uds_init(&hb_uds, false)) != 0) {
		hb_uds = NULL;
		return rc;
	}
	if ((rc = uds_register_client(hb_uds, "hyperberry_hub", "", assets)) != 0) {
		uds_close(&hb_uds);
		hb_uds = NULL;
		return rc;
	}
	*fresh = uds_get_service_status(hb_uds) == UDS_REGISTRATION_NEW;
	return 0;
}

// hb_uds_action registers one of the item menu's actions, shown for the items whose context
// state has a bit of mask. They go to the engine's headless target.
static int hb_uds_action(long long id, const char *action, const char *title, const char *image,
		unsigned int mask, uds_placement_type_t placement) {
	uds_item_action_data_t *a = uds_item_action_data_create();
	int rc;
	uds_item_action_data_set_action(a, action);
	uds_item_action_data_set_target(a, "com.zsavajji.hyperberry.service");
	uds_item_action_data_set_type(a, "service");
	uds_item_action_data_set_title(a, title);
	uds_item_action_data_set_image_source(a, image);
	uds_item_action_data_set_mime_type(a, HB_MIME);
	uds_item_action_data_set_placement(a, placement);
	uds_item_action_data_set_context_mask(a, mask);
	rc = uds_register_item_context_action(hb_uds, id, a);
	uds_item_action_data_destroy(a);
	return rc;
}

// hb_uds_account adds the account with its three item actions, or removes it with its items.
// An account that is already there is updated, and its actions are left as they are.
static int hb_uds_account(long long id, int add, const char *markRead, const char *markUnread,
		const char *del) {
	uds_account_data_t *a;
	int rc;
	if (!add)
		return uds_account_removed(hb_uds, id);
	a = uds_account_data_create();
	uds_account_data_set_id(a, id);
	uds_account_data_set_name(a, "HyperBerry");
	uds_account_data_set_description(a, "WhatsApp");
	uds_account_data_set_icon(a, "hub.png"); // white: the lock screen and the instant preview draw it
	uds_account_data_set_target_name(a, "com.zsavajji.hyperberry");
	uds_account_data_set_supports_compose(a, false);
	uds_account_data_set_type(a, UDS_ACCOUNT_TYPE_IM);
	if ((rc = uds_account_added(hb_uds, a)) != 0) {
		rc = uds_account_updated(hb_uds, a);
		uds_account_data_destroy(a);
		return rc;
	}
	uds_account_data_destroy(a);
	if ((rc = hb_uds_action(id, "com.zsavajji.hyperberry.MARKREAD", markRead, "tick_action.png", 1,
			UDS_PLACEMENT_DEFAULT)) != 0)
		return rc;
	if ((rc = hb_uds_action(id, "com.zsavajji.hyperberry.MARKUNREAD", markUnread, "unread_action.png", 2,
			UDS_PLACEMENT_DEFAULT)) != 0)
		return rc;
	// FIXED is the placement for a delete: without this action the Hub's Delete does nothing.
	return hb_uds_action(id, "com.zsavajji.hyperberry.DELETE", del, "delete_action.png", 3,
		UDS_PLACEMENT_FIXED);
}

// hb_uds_item adds or updates one chat's inbox item. The source id rides in the user data too.
static int hb_uds_item(long long account, int add, const char *source, const char *name,
		const char *desc, int unread, long long ts, int alert) {
	uds_inbox_item_data_t *i = uds_inbox_item_data_create();
	int rc;
	uds_inbox_item_data_set_account_id(i, account);
	uds_inbox_item_data_set_source_id(i, (uds_source_key_t)source);
	uds_inbox_item_data_set_user_data(i, source);
	uds_inbox_item_data_set_name(i, name);
	uds_inbox_item_data_set_description(i, desc);
	// The Hub is light (a white glyph vanished, Q10, 0.0.1.100) and tints nothing: yellow unread, grey read.
	uds_inbox_item_data_set_icon(i, unread > 0 ? "hub_unread.png" : "hub_read.png");
	uds_inbox_item_data_set_mime_type(i, HB_MIME);
	uds_inbox_item_data_set_unread_count(i, unread);
	uds_inbox_item_data_set_total_count(i, 1);
	uds_inbox_item_data_set_timestamp(i, ts);
	uds_inbox_item_data_set_context_state(i, unread > 0 ? 1 : 2);
	uds_inbox_item_data_set_notification_state(i, alert);
	rc = add ? uds_item_added(hb_uds, i) : uds_item_updated(hb_uds, i);
	uds_inbox_item_data_destroy(i);
	return rc;
}

// hb_uds_remove takes one chat's item out of the Hub; the chat stays in the app.
static int hb_uds_remove(long long account, const char *source) {
	return uds_item_removed(hb_uds, account, (uds_source_key_t)source);
}

// hb_italian is 1 when the phone's language is Italian.
static int hb_italian(void) {
	char *lang = NULL, *country = NULL;
	int it = 0;
	if (bps_initialize() != BPS_SUCCESS)
		return 0;
	if (locale_get(&lang, &country) == BPS_SUCCESS && lang && strcmp(lang, "it") == 0)
		it = 1;
	if (lang)
		bps_free(lang);
	if (country)
		bps_free(country);
	bps_shutdown();
	return it;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func udsErr(rc C.int) error {
	if rc == 0 {
		return nil
	}
	return fmt.Errorf("uds %d", int(rc))
}

func cbool(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// udsOpen registers the app with the Hub, whose view of our public assets (assets) it needs.
// fresh is true when the Hub had never seen the app, so nothing of ours is in it.
func udsOpen(assets string) (fresh bool, err error) {
	a := C.CString(assets)
	defer C.free(unsafe.Pointer(a))
	var f C.int
	if err := udsErr(C.hb_uds_open(a, &f)); err != nil {
		return false, err
	}
	return f != 0, nil
}

// udsAccount adds the Hub account, with the item menu's actions, or removes it with its items.
// ponytail: the action titles follow the phone's language when the account is added; a later
// change shows the old ones until a re-link.
func udsAccount(add bool) error {
	read, unread, del := "Mark as read", "Mark as unread", "Delete"
	if add && C.hb_italian() != 0 {
		read, unread, del = "Segna come letta", "Segna come da leggere", "Elimina"
	}
	r, u, d := C.CString(read), C.CString(unread), C.CString(del)
	defer C.free(unsafe.Pointer(r))
	defer C.free(unsafe.Pointer(u))
	defer C.free(unsafe.Pointer(d))
	return udsErr(C.hb_uds_account(C.longlong(hubAccount), cbool(add), r, u, d))
}

// udsItem adds or, with add false, updates one chat's item.
func udsItem(it hubItem, add bool) error {
	id, title, body := C.CString(it.id), C.CString(it.title), C.CString(it.body)
	defer C.free(unsafe.Pointer(id))
	defer C.free(unsafe.Pointer(title))
	defer C.free(unsafe.Pointer(body))
	return udsErr(C.hb_uds_item(C.longlong(hubAccount), cbool(add), id, title, body,
		C.int(it.unread), C.longlong(it.ts), cbool(it.alert)))
}

// udsRemove takes one chat's item out of the Hub.
func udsRemove(id string) error {
	s := C.CString(id)
	defer C.free(unsafe.Pointer(s))
	return udsErr(C.hb_uds_remove(C.longlong(hubAccount), s))
}
