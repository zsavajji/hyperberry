package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// Message translation (docs/plan/translate.md): one function per service, each translating one text.
// The text leaves WhatsApp's encryption for the service the user picked, so nothing here logs a text or a key.

// Variables so the tests point them at httptest servers.
var (
	googleURL    = "https://translate.googleapis.com/translate_a/single"
	googleAPIURL = "https://translation.googleapis.com/language/translate/v2" // with a key: the official API
	deeplFreeURL = "https://api-free.deepl.com"
	deeplProURL  = "https://api.deepl.com"
)

// Google's endpoint answers Go's HTTP/2 client with 429 "Sorry..." at the first request, whatever the User-Agent,
// and the same request over HTTP/1.1 with 200 (seen from the Mac and from Docker, 2026-11-20): so its client has
// HTTP/2 off. A transport of its own, not a clone of the default one, whose TLS config already offers h2 once it
// was used. The other services take the default client.
var googleClient = &http.Client{Transport: &http.Transport{
	TLSNextProto:        map[string]func(string, *tls.Conn) http.RoundTripper{},
	TLSHandshakeTimeout: 10 * time.Second,
	MaxIdleConnsPerHost: 4, // translate_msg.go's requests at once
	IdleConnTimeout:     90 * time.Second,
}}

const (
	transTimeout = 15 * time.Second
	transBodyMax = 1 << 20
	// ponytail: Google's keyless endpoint takes about 5000 characters a request; longer texts go in chunks of 4500 runes
	// at line breaks, one request each, in order. A text of many chunks is slow and may throttle: raise the ceiling
	// or batch only if long messages turn out common.
	googleChunk = 4500
)

// transSettings is what the user picked in Settings > Translation.
type transSettings struct{ provider, deeplKey, libreURL, libreKey, googleKey string }

// translateText translates one text into to. Its result is empty when there is nothing to show: the text has no
// letter (it is never sent) or the service found it already in to. from is the detected language, empty when unknown.
func translateText(ctx context.Context, ts transSettings, text, to string) (out, from string, err error) {
	if strings.IndexFunc(text, unicode.IsLetter) < 0 {
		return "", "", nil
	}
	if err := transPaused(ts.variant()); err != nil {
		return "", "", err
	}
	switch ts.provider {
	case "deepl":
		out, from, err = translateDeepL(ctx, ts, text, to)
	case "libre":
		out, from, err = translateLibre(ctx, ts, text, to)
	case "google":
		if ts.googleKey != "" {
			out, from, err = translateGoogleAPI(ctx, ts, text, to)
			break
		}
		fallthrough
	default:
		out, from, err = translateGoogle(ctx, text, to)
	}
	if err == nil && langBase(from) == langBase(to) {
		out = ""
	}
	return out, from, err
}

// variant names the service for the cooldown: a key, or another service, is not held back by the other's pause.
func (ts transSettings) variant() string {
	switch {
	case ts.provider == "deepl", ts.provider == "libre":
		return ts.provider
	case ts.googleKey != "":
		return "google+key"
	}
	return "google"
}

// The cooldown: a service that said too many requests is left alone, not asked again by every request. The pause is
// the answer's Retry-After, else a wait of 60 s that doubles at each consecutive refusal up to 15 minutes; a success
// clears it.
const (
	coolStart = 60 * time.Second
	coolMax   = 15 * time.Minute
)

type cool struct {
	until time.Time
	next  time.Duration
}

var (
	transNow              = time.Now // tests move time
	transLog waLog.Logger = waLog.Noop
	coolMu   sync.Mutex
	cools    = map[string]*cool{}
)

func transPaused(variant string) error {
	coolMu.Lock()
	defer coolMu.Unlock()
	if c := cools[variant]; c != nil && transNow().Before(c.until) {
		return &Error{"rate_limited", "translate: waiting, the service said too many requests"}
	}
	return nil
}

// transLimited starts a pause after a rate_limited answer. One that comes while a pause runs (a request that was
// already in flight) changes nothing, so four at once don't count as four refusals.
func transLimited(variant string, retryAfter time.Duration) {
	coolMu.Lock()
	defer coolMu.Unlock()
	c := cools[variant]
	if c == nil {
		c = &cool{}
		cools[variant] = c
	}
	if transNow().Before(c.until) {
		return
	}
	wait := retryAfter
	if wait <= 0 {
		wait = max(c.next, coolStart)
		c.next = min(wait*2, coolMax)
	}
	c.until = transNow().Add(wait)
	transLog.Infof("translate: %s paused for %d s", variant, int(wait.Seconds()))
}

func transOK(variant string) {
	coolMu.Lock()
	delete(cools, variant)
	coolMu.Unlock()
}

// langBase is the language of a code, without its region: "pt-BR" is "pt".
func langBase(s string) string {
	b, _, _ := strings.Cut(s, "-")
	b, _, _ = strings.Cut(b, "_")
	return strings.ToLower(b)
}

// byStatus is a mapper of failures that goes by the HTTP status alone.
func byStatus(m map[int]string) func(int, []byte) string {
	return func(status int, _ []byte) string { return m[status] }
}

// postTrans posts one request and returns the body of a 200. No answer is offline; a status that is not 200 is the
// code the mapper gives for its status and body, else translate_failed with the status. A rate_limited answer starts
// the variant's pause, a 200 clears it.
func postTrans(ctx context.Context, cli *http.Client, variant, u, ctype string, hdr map[string]string, body []byte, codes func(int, []byte) string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, transTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, &Error{"translate_failed", "translate: bad service address"}
	}
	req.Header.Set("Content-Type", ctype)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, &Error{"offline", "translate: no answer from the service"}
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, transBodyMax))
	if err != nil {
		return nil, &Error{"offline", "translate: the answer broke off"}
	}
	if resp.StatusCode != http.StatusOK {
		c := codes(resp.StatusCode, b)
		if c == "" {
			c = "translate_failed"
		}
		if c == "rate_limited" {
			secs, _ := strconv.Atoi(resp.Header.Get("Retry-After")) // an HTTP date is ignored
			transLimited(variant, time.Duration(secs)*time.Second)
		}
		return nil, &Error{c, fmt.Sprintf("translate: HTTP %d", resp.StatusCode)}
	}
	transOK(variant)
	return b, nil
}

var badAnswer = &Error{"translate_failed", "translate: the answer is not what the service sends"}

// chunkText cuts text at line breaks into pieces of at most max runes; a single longer line is cut hard.
func chunkText(text string, max int) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = nil
		}
	}
	for i, line := range strings.Split(text, "\n") {
		l := []rune(line)
		if i > 0 {
			l = append([]rune{'\n'}, l...)
		}
		if len(cur)+len(l) > max {
			flush()
		}
		for len(l) > max {
			out = append(out, string(l[:max]))
			l = l[max:]
		}
		cur = append(cur, l...)
	}
	flush()
	return out
}

func translateGoogle(ctx context.Context, text, to string) (out, from string, err error) {
	var sb strings.Builder
	for i, chunk := range chunkText(text, googleChunk) {
		piece, f, err := googleOnce(ctx, chunk, to)
		if err != nil {
			return "", "", err
		}
		if i == 0 {
			from = f
		}
		sb.WriteString(piece)
	}
	return sb.String(), from, nil
}

func googleOnce(ctx context.Context, text, to string) (out, from string, err error) {
	q := url.Values{"client": {"gtx"}, "sl": {"auto"}, "tl": {to}, "dt": {"t"}}
	b, err := postTrans(ctx, googleClient, "google", googleURL+"?"+q.Encode(), "application/x-www-form-urlencoded",
		map[string]string{"User-Agent": "Mozilla/5.0 (Linux; Android 10) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36"},
		[]byte(url.Values{"q": {text}}.Encode()), byStatus(map[int]string{429: "rate_limited"}))
	if err != nil {
		return "", "", err
	}
	// [[["piece","original",...], ...], null, "en", ...]
	var v []any
	if json.Unmarshal(b, &v) != nil || len(v) < 3 {
		return "", "", badAnswer
	}
	segs, _ := v[0].([]any)
	var sb strings.Builder
	for _, s := range segs {
		if seg, _ := s.([]any); len(seg) > 0 {
			if p, ok := seg[0].(string); ok {
				sb.WriteString(p)
			}
		}
	}
	from, _ = v[2].(string)
	return sb.String(), from, nil
}

// googleReasons are the error reasons and statuses of the official API that mean a limit was reached.
var googleReasons = map[string]bool{"rateLimitExceeded": true, "userRateLimitExceeded": true, "dailyLimitExceeded": true,
	"quotaExceeded": true, "RESOURCE_EXHAUSTED": true, "RATE_LIMIT_EXCEEDED": true}

// googleAPIFail maps the official API's failures: a limit is rate_limited; a bad key, or any other 403 (the API is
// not enabled for the key, or the key is restricted), is no_key; any other 400 is unsupported.
func googleAPIFail(status int, body []byte) string {
	var e struct {
		Error struct {
			Status  string
			Errors  []struct{ Reason string }
			Details []struct{ Reason string }
		}
	}
	json.Unmarshal(body, &e)
	limited, badKey := status == 429 || googleReasons[e.Error.Status], false
	for _, r := range e.Error.Errors {
		limited = limited || googleReasons[r.Reason]
	}
	for _, r := range e.Error.Details {
		limited = limited || googleReasons[r.Reason]
		badKey = badKey || r.Reason == "API_KEY_INVALID"
	}
	switch {
	case limited:
		return "rate_limited"
	case badKey, status == 403:
		return "no_key"
	case status == 400:
		return "unsupported"
	}
	return ""
}

// translateGoogleAPI is Google with the user's key: Cloud Translation v2. The key goes in a header, never in the
// address, so no error or log can carry it. format is text: the default, HTML, would escape the text. No chunking:
// the official API takes long texts.
func translateGoogleAPI(ctx context.Context, ts transSettings, text, to string) (out, from string, err error) {
	body, _ := json.Marshal(map[string]any{"q": []string{text}, "target": to, "format": "text"})
	b, err := postTrans(ctx, http.DefaultClient, "google+key", googleAPIURL, "application/json",
		map[string]string{"X-Goog-Api-Key": ts.googleKey}, body, googleAPIFail)
	if err != nil {
		return "", "", err
	}
	type tr struct{ TranslatedText, DetectedSourceLanguage string }
	var r struct {
		Data         struct{ Translations []tr }
		Translations []tr // the discovery document shows it unwrapped
	}
	if json.Unmarshal(b, &r) != nil {
		return "", "", badAnswer
	}
	list := append(r.Data.Translations, r.Translations...)
	if len(list) == 0 {
		return "", "", badAnswer
	}
	return list[0].TranslatedText, list[0].DetectedSourceLanguage, nil
}

func translateDeepL(ctx context.Context, ts transSettings, text, to string) (out, from string, err error) {
	if ts.deeplKey == "" {
		return "", "", &Error{"no_key", "translate: DeepL needs a key"}
	}
	target := strings.ToUpper(to)
	switch langBase(to) {
	case "en":
		target = "EN-US"
	case "pt":
		target = "PT-PT"
	}
	base := deeplProURL
	if strings.HasSuffix(ts.deeplKey, ":fx") {
		base = deeplFreeURL
	}
	body, _ := json.Marshal(map[string]any{"text": []string{text}, "target_lang": target})
	b, err := postTrans(ctx, http.DefaultClient, "deepl", base+"/v2/translate", "application/json", map[string]string{"Authorization": "DeepL-Auth-Key " + ts.deeplKey},
		body, byStatus(map[int]string{403: "no_key", 429: "rate_limited", 456: "rate_limited", 400: "unsupported"}))
	if err != nil {
		return "", "", err
	}
	var r struct {
		Translations []struct {
			Detected string `json:"detected_source_language"`
			Text     string `json:"text"`
		}
	}
	if json.Unmarshal(b, &r) != nil || len(r.Translations) == 0 {
		return "", "", badAnswer
	}
	return r.Translations[0].Text, strings.ToLower(r.Translations[0].Detected), nil
}

func translateLibre(ctx context.Context, ts transSettings, text, to string) (out, from string, err error) {
	if ts.libreURL == "" {
		return "", "", &Error{"no_key", "translate: LibreTranslate needs a server address"}
	}
	req := map[string]any{"q": text, "source": "auto", "target": to, "format": "text"}
	if ts.libreKey != "" {
		req["api_key"] = ts.libreKey
	}
	body, _ := json.Marshal(req)
	b, err := postTrans(ctx, http.DefaultClient, "libre", strings.TrimSuffix(ts.libreURL, "/")+"/translate", "application/json", nil,
		body, byStatus(map[int]string{400: "unsupported", 403: "no_key", 429: "rate_limited"}))
	if err != nil {
		return "", "", err
	}
	var r struct {
		TranslatedText   string
		DetectedLanguage struct{ Language string }
	}
	if json.Unmarshal(b, &r) != nil {
		return "", "", badAnswer
	}
	return r.TranslatedText, r.DetectedLanguage.Language, nil
}
