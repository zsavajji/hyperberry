package main

import (
	"encoding/json"
	"time"
)

// prefs is what the UI sets with prefs.set, kept in prefs.json: the engine needs it itself,
// since it notifies and words previews while the UI is closed. A nil field was never set.
type prefs struct {
	NotifyPreview *bool   `json:"notifyPreview,omitempty"`
	MediaOnMobile *bool   `json:"mediaOnMobile,omitempty"`
	AutoPhoto     *bool   `json:"autoPhoto,omitempty"`
	AutoAudio     *bool   `json:"autoAudio,omitempty"`
	AutoVideo     *bool   `json:"autoVideo,omitempty"`
	AutoDoc       *bool   `json:"autoDoc,omitempty"`
	Language      *string `json:"language,omitempty"`
	CacheMB       *int    `json:"cacheMB,omitempty"`
	CloudflareDNS *bool   `json:"cloudflareDNS,omitempty"` // off until set: the network's own DNS
	// The translation service (translate.go): google, deepl or libre; google until set.
	TranslateProvider *string `json:"translateProvider,omitempty"`
	DeeplKey          *string `json:"deeplKey,omitempty"`
	LibreURL          *string `json:"libreURL,omitempty"`
	LibreKey          *string `json:"libreKey,omitempty"`
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

// autoDownload is whether a live message of this kind downloads by itself: photos and audio (voice notes and
// audio files) on until set otherwise, videos and documents off. A sticker always does: it is small, and the
// chat fetches it as it draws it. Anything else has no file.
func (n *Notifier) autoDownload(kind int) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	switch kind {
	case msgPhoto:
		return n.prefs.AutoPhoto == nil || *n.prefs.AutoPhoto
	case msgVoice, msgAudio:
		return n.prefs.AutoAudio == nil || *n.prefs.AutoAudio
	case msgVideo:
		return n.prefs.AutoVideo != nil && *n.prefs.AutoVideo
	case msgDoc:
		return n.prefs.AutoDoc != nil && *n.prefs.AutoDoc
	}
	return kind == msgSticker
}

// cacheLimit is what data/cache/ may hold, in bytes.
func (n *Notifier) cacheLimit() int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.prefs.CacheMB == nil {
		return cacheDefaultMB << 20
	}
	return int64(*n.prefs.CacheMB) << 20
}

// translateSettings is the service the user picked and its keys, with the phone's language as the default target.
func (n *Notifier) translateSettings() (ts transSettings, lang string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	ts.provider = "google"
	if p := n.prefs.TranslateProvider; p != nil {
		ts.provider = *p
	}
	if n.prefs.DeeplKey != nil {
		ts.deeplKey = *n.prefs.DeeplKey
	}
	if n.prefs.LibreURL != nil {
		ts.libreURL = *n.prefs.LibreURL
	}
	if n.prefs.LibreKey != nil {
		ts.libreKey = *n.prefs.LibreKey
	}
	if n.prefs.Language != nil {
		lang = *n.prefs.Language
	}
	return ts, lang
}

// setPrefs merges the fields prefs.set gives into the stored ones and writes the lot. langChanged
// is true when the language differs from the stored one, so the previews need wording again.
func (n *Notifier) setPrefs(req json.RawMessage) (langChanged bool, err error) {
	var r prefs
	if json.Unmarshal(req, &r) != nil || r.NotifyPreview == nil && r.MediaOnMobile == nil && r.AutoPhoto == nil && r.AutoAudio == nil &&
		r.AutoVideo == nil && r.AutoDoc == nil && r.Language == nil && r.CacheMB == nil && r.CloudflareDNS == nil &&
		r.TranslateProvider == nil && r.DeeplKey == nil && r.LibreURL == nil && r.LibreKey == nil {
		return false, &Error{"bad_request", "prefs.set needs notifyPreview, mediaOnMobile, autoPhoto, autoAudio, autoVideo, autoDoc, language, cacheMB, cloudflareDNS, translateProvider, deeplKey, libreURL or libreKey"}
	}
	if r.CacheMB != nil && (*r.CacheMB < cacheDefaultMB || *r.CacheMB > cacheMaxMB) {
		return false, &Error{"bad_request", "cacheMB is 64 to 512"}
	}
	if p := r.TranslateProvider; p != nil && *p != "google" && *p != "deepl" && *p != "libre" {
		return false, &Error{"bad_request", "translateProvider is google, deepl or libre"}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if r.NotifyPreview != nil {
		n.prefs.NotifyPreview = r.NotifyPreview
	}
	if r.MediaOnMobile != nil {
		n.prefs.MediaOnMobile = r.MediaOnMobile
	}
	if r.AutoPhoto != nil {
		n.prefs.AutoPhoto = r.AutoPhoto
	}
	if r.AutoAudio != nil {
		n.prefs.AutoAudio = r.AutoAudio
	}
	if r.AutoVideo != nil {
		n.prefs.AutoVideo = r.AutoVideo
	}
	if r.AutoDoc != nil {
		n.prefs.AutoDoc = r.AutoDoc
	}
	if r.Language != nil {
		langChanged = n.prefs.Language == nil || *n.prefs.Language != *r.Language
		n.prefs.Language = r.Language
		setLanguage(*r.Language)
	}
	if r.CacheMB != nil {
		n.prefs.CacheMB = r.CacheMB
	}
	if r.CloudflareDNS != nil {
		n.prefs.CloudflareDNS = r.CloudflareDNS
		useCloudflareDNS(*r.CloudflareDNS)
	}
	if r.TranslateProvider != nil {
		n.prefs.TranslateProvider = r.TranslateProvider
	}
	if r.DeeplKey != nil {
		n.prefs.DeeplKey = r.DeeplKey
	}
	if r.LibreURL != nil {
		n.prefs.LibreURL = r.LibreURL
	}
	if r.LibreKey != nil {
		n.prefs.LibreKey = r.LibreKey
	}
	b, _ := json.Marshal(n.prefs)
	return langChanged, writeAtomic(n.path, b)
}

// handlePrefs is prefs.set. A new language rewrites every chat's preview, in the writer, and a limit
// that came down takes effect at once. Pruning at every prefs.set is only a walk of the folder.
func (st *Store) handlePrefs(req json.RawMessage) (any, error) {
	langChanged, err := st.nt.setPrefs(req)
	if err == nil && langChanged {
		st.ops <- func(t *storeTx) { t.rewritePreviews() }
	}
	if err == nil {
		go pruneCache(st.srv.dir, st.nt.cacheLimit(), "", time.Now(), st.log)
	}
	return nil, err
}
