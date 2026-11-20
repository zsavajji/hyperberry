package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

func TestTranslateGoogleAPI(t *testing.T) {
	defer func(u string) { googleAPIURL = u }(googleAPIURL)
	ctx := context.Background()
	ts := transSettings{provider: "google", googleKey: "SECRET"}
	s := newStub(t, 200, `{"data":{"translations":[{"translatedText":"Ciao &amp; co","detectedSourceLanguage":"en"}]}}`)
	googleAPIURL = s.URL + "/language/translate/v2"
	out, from, err := translateText(ctx, ts, "Hello & co", "it")
	if err != nil || out != "Ciao &amp; co" || from != "en" {
		t.Fatalf("%q %q %v", out, from, err)
	}
	if s.method != "POST" || s.path != "/language/translate/v2" || s.query != "" || s.ctype != "application/json" ||
		s.body != `{"format":"text","q":["Hello \u0026 co"],"target":"it"}` || s.key != "SECRET" || s.n.Load() != 1 {
		t.Errorf("request: %+v", s)
	}
	// Unwrapped answer.
	s = newStub(t, 200, `{"translations":[{"translatedText":"Hallo","detectedSourceLanguage":"en"}]}`)
	googleAPIURL = s.URL
	if out, from, err = translateText(ctx, ts, "Hello", "de"); err != nil || out != "Hallo" || from != "en" {
		t.Errorf("unwrapped: %q %q %v", out, from, err)
	}
	// No answer of the shape: a failure, not an empty translation.
	googleAPIURL = newStub(t, 200, `{"data":{}}`).URL
	if _, _, err = translateText(ctx, ts, "Hello", "de"); code(err) != "translate_failed" {
		t.Errorf("empty: %v", err)
	}
	for _, c := range []struct {
		status     int
		body, want string
	}{
		{429, ``, "rate_limited"},
		{403, `{"error":{"code":403,"status":"PERMISSION_DENIED","errors":[{"reason":"dailyLimitExceeded"}]}}`, "rate_limited"},
		{403, `{"error":{"code":403,"status":"RESOURCE_EXHAUSTED"}}`, "rate_limited"},
		{400, `{"error":{"code":400,"details":[{"reason":"rateLimitExceeded"}]}}`, "rate_limited"},
		{400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","errors":[{"reason":"badRequest"}],"details":[{"reason":"API_KEY_INVALID"}]}}`, "no_key"},
		{403, `{"error":{"code":403,"status":"PERMISSION_DENIED"}}`, "no_key"},
		{403, `not json`, "no_key"},
		{400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","errors":[{"reason":"badRequest"}]}}`, "unsupported"},
		{500, `{"error":{"code":500}}`, "translate_failed"},
	} {
		googleAPIURL = newStub(t, c.status, c.body).URL
		_, _, err := translateText(ctx, ts, "Hello", "de")
		if code(err) != c.want || strings.Contains(err.Error(), "SECRET") {
			t.Errorf("HTTP %d %s: %v, want %s", c.status, c.body, err, c.want)
		}
	}
	// Keyless stays the keyless endpoint.
	k := newStub(t, 200, `[[["x"]],null,"en"]`)
	defer func(u string) { googleURL = u }(googleURL)
	googleURL = k.URL
	if _, _, err := translateText(ctx, transSettings{provider: "google"}, "Hello", "it"); err != nil || k.n.Load() != 1 || k.key != "" {
		t.Errorf("keyless: %v %+v", err, k)
	}
}

func TestTranslateCooldown(t *testing.T) {
	resetCool()
	t.Cleanup(resetCool)
	now := time.Unix(1000, 0)
	transNow = func() time.Time { return now }
	ctx := context.Background()
	var status atomic.Int32
	status.Store(429)
	var retry atomic.Value
	retry.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ra := retry.Load().(string); ra != "" {
			w.Header().Set("Retry-After", ra)
		}
		w.WriteHeader(int(status.Load()))
		if status.Load() == 200 {
			io.WriteString(w, `{"translatedText":"Ciao","detectedLanguage":{"language":"en"}}`)
		}
	}))
	defer srv.Close()
	libre := transSettings{provider: "libre", libreURL: srv.URL}
	var n atomic.Int32
	counted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		srv.Config.Handler.ServeHTTP(w, r)
	}))
	defer counted.Close()
	libre.libreURL = counted.URL
	ask := func(ts transSettings) error {
		_, _, err := translateText(ctx, ts, "Hello", "it")
		return err
	}
	pause := func() time.Duration {
		coolMu.Lock()
		defer coolMu.Unlock()
		return cools["libre"].until.Sub(now)
	}

	// 429: a pause of 60 s; during it nothing is sent.
	if code(ask(libre)) != "rate_limited" || n.Load() != 1 || pause() != 60*time.Second {
		t.Fatalf("first 429: %d requests, pause %v", n.Load(), pause())
	}
	now = now.Add(59 * time.Second)
	if err := ask(libre); code(err) != "rate_limited" || n.Load() != 1 || !strings.Contains(err.Error(), "waiting") {
		t.Errorf("during the pause: %v, %d requests", err, n.Load())
	}
	// Another variant is not held back.
	for _, v := range []string{"google", "google+key", "deepl"} {
		if transPaused(v) != nil {
			t.Errorf("%s waits for libre's pause", v)
		}
	}
	// The wait doubles at each consecutive 429, and caps at 15 minutes.
	for _, want := range []time.Duration{120, 240, 480, 900, 900} {
		now = now.Add(time.Hour)
		ask(libre)
		if pause() != want*time.Second {
			t.Errorf("pause %v, want %v s", pause(), want)
		}
	}
	// Retry-After in seconds wins; an HTTP date is ignored.
	now = now.Add(time.Hour)
	retry.Store("7")
	ask(libre)
	if pause() != 7*time.Second {
		t.Errorf("Retry-After 7: %v", pause())
	}
	now = now.Add(time.Hour)
	retry.Store("Wed, 21 Oct 2026 07:28:00 GMT")
	ask(libre)
	if pause() != 900*time.Second {
		t.Errorf("Retry-After date: %v", pause())
	}
	// A success clears the pause and the wait: the next refusal is 60 s again.
	now = now.Add(time.Hour)
	status.Store(200)
	before := n.Load()
	if err := ask(libre); err != nil || n.Load() != before+1 {
		t.Fatalf("after the pause: %v", err)
	}
	status.Store(429)
	retry.Store("")
	ask(libre)
	if pause() != 60*time.Second {
		t.Errorf("after a success: %v", pause())
	}
}

func TestPrefsGoogleKey(t *testing.T) {
	n := newNotifier(t.TempDir(), waLog.Noop)
	defer n.close()
	if ts, _ := n.translateSettings(); ts.googleKey != "" {
		t.Error("a key out of nothing")
	}
	if _, err := n.setPrefs([]byte(`{"googleKey":"abc"}`)); err != nil {
		t.Fatal(err)
	}
	if ts, _ := n.translateSettings(); ts.googleKey != "abc" || ts.variant() != "google+key" {
		t.Errorf("%+v", ts)
	}
	if _, err := n.setPrefs([]byte(`{"googleKey":""}`)); err != nil {
		t.Fatal(err)
	}
	if ts, _ := n.translateSettings(); ts.variant() != "google" {
		t.Errorf("%+v", ts)
	}
}
