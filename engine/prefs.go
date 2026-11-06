package main

import (
	"encoding/json"
)

// prefs is what the UI sets with prefs.set, kept in prefs.json: the engine needs it itself,
// since it notifies and words previews while the UI is closed. A nil field was never set.
type prefs struct {
	NotifyPreview *bool   `json:"notifyPreview,omitempty"`
	MediaOnMobile *bool   `json:"mediaOnMobile,omitempty"`
	Language      *string `json:"language,omitempty"`
}

// notifyPreview is whether the Hub shows the message text; on until set otherwise.
func (n *Notifier) notifyPreview() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.prefs.NotifyPreview == nil || *n.prefs.NotifyPreview
}

// mediaOnMobile is whether live media downloads on mobile data; on until set otherwise.
func (n *Notifier) mediaOnMobile() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.prefs.MediaOnMobile == nil || *n.prefs.MediaOnMobile
}

// setPrefs merges the fields prefs.set gives into the stored ones and writes the lot. langChanged
// is true when the language differs from the stored one, so the previews need wording again.
func (n *Notifier) setPrefs(req json.RawMessage) (langChanged bool, err error) {
	var r prefs
	if json.Unmarshal(req, &r) != nil || r.NotifyPreview == nil && r.MediaOnMobile == nil && r.Language == nil {
		return false, &Error{"bad_request", "prefs.set needs notifyPreview, mediaOnMobile or language"}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if r.NotifyPreview != nil {
		n.prefs.NotifyPreview = r.NotifyPreview
	}
	if r.MediaOnMobile != nil {
		n.prefs.MediaOnMobile = r.MediaOnMobile
	}
	if r.Language != nil {
		langChanged = n.prefs.Language == nil || *n.prefs.Language != *r.Language
		n.prefs.Language = r.Language
		setLanguage(*r.Language)
	}
	b, _ := json.Marshal(n.prefs)
	return langChanged, writeAtomic(n.path, b)
}

// handlePrefs is prefs.set. A new language rewrites every chat's preview, in the writer.
func (st *Store) handlePrefs(req json.RawMessage) (any, error) {
	langChanged, err := st.nt.setPrefs(req)
	if err == nil && langChanged {
		st.ops <- func(t *storeTx) { t.rewritePreviews() }
	}
	return nil, err
}
