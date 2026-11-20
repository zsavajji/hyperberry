package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"

	waLog "github.com/polymorfa/hypermeow/util/log"
	"testing"
)

// stub answers every request with status and body, and keeps the last request's shape.
type stub struct {
	*httptest.Server
	method, path, query, ctype, auth, ua, body string
	n                                          atomic.Int32
}

func newStub(t *testing.T, status int, reply string) *stub {
	s := &stub{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.method, s.path, s.query, s.ctype = r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Content-Type")
		s.auth, s.ua, s.body = r.Header.Get("Authorization"), r.Header.Get("User-Agent"), string(b)
		s.n.Add(1)
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestTranslateGoogle(t *testing.T) {
	s := newStub(t, 200, `[[["Ciao ","Hello ",null,null,10],["mondo\n","world\n"],[null,null],["!"]],null,"en"]`)
	defer func(u string) { googleURL = u }(googleURL)
	googleURL = s.URL + "/translate_a/single"
	out, from, err := translateText(context.Background(), transSettings{provider: "google"}, "Hello world & co\n", "it")
	if err != nil || out != "Ciao mondo\n!" || from != "en" {
		t.Fatalf("%q %q %v", out, from, err)
	}
	if s.method != "POST" || s.path != "/translate_a/single" || s.query != "client=gtx&dt=t&sl=auto&tl=it" ||
		s.ctype != "application/x-www-form-urlencoded" || s.body != "q=Hello+world+%26+co%0A" || !strings.HasPrefix(s.ua, "Mozilla/") {
		t.Errorf("request: %+v", s)
	}
	for status, want := range map[int]string{429: "rate_limited", 500: "translate_failed"} {
		e := newStub(t, status, "")
		googleURL = e.URL
		if _, _, err := translateText(context.Background(), transSettings{}, "Hello", "it"); code(err) != want {
			t.Errorf("HTTP %d: %v, want %s", status, err, want)
		}
	}
	e := newStub(t, 200, `{"not":"an array"}`)
	googleURL = e.URL
	if _, _, err := translateText(context.Background(), transSettings{}, "Hello", "it"); code(err) != "translate_failed" {
		t.Errorf("odd answer: %v", err)
	}
}

func TestGoogleOffline(t *testing.T) {
	s := newStub(t, 200, "")
	s.Close()
	defer func(u string) { googleURL = u }(googleURL)
	googleURL = s.URL
	if _, _, err := translateText(context.Background(), transSettings{}, "Hello", "it"); code(err) != "offline" {
		t.Errorf("closed server: %v", err)
	}
}

func TestChunkText(t *testing.T) {
	for _, c := range []struct {
		in   string
		max  int
		want []string
	}{
		{"abc", 5, []string{"abc"}},
		{"ab\ncd\nef", 5, []string{"ab\ncd", "\nef"}},
		{"abcdefghijk", 4, []string{"abcd", "efgh", "ijk"}},
		{"ab\ncdefghi\nj", 4, []string{"ab", "\ncde", "fghi", "\nj"}},
		{"èèè\nèè", 3, []string{"èèè", "\nèè"}},
	} {
		got := chunkText(c.in, c.max)
		if strings.Join(got, "|") != strings.Join(c.want, "|") || strings.Join(got, "") != c.in {
			t.Errorf("%q/%d: %q, want %q", c.in, c.max, got, c.want)
		}
	}
	// Long text: several requests, in order, joined.
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		n.Add(1)
		json.NewEncoder(w).Encode([]any{[]any{[]any{strings.ToUpper(r.PostForm.Get("q"))}}, nil, "en"})
	}))
	defer srv.Close()
	defer func(u string) { googleURL = u }(googleURL)
	googleURL = srv.URL
	long := strings.Repeat("a", googleChunk) + "\n" + strings.Repeat("b", 10)
	out, _, err := translateText(context.Background(), transSettings{}, long, "it")
	if err != nil || out != strings.ToUpper(long) || n.Load() != 2 {
		t.Errorf("long text: %d requests, err %v, same %v", n.Load(), err, out == strings.ToUpper(long))
	}
}

func TestTranslateDeepL(t *testing.T) {
	defer func(a, b string) { deeplFreeURL, deeplProURL = a, b }(deeplFreeURL, deeplProURL)
	free := newStub(t, 200, `{"translations":[{"detected_source_language":"EN","text":"Ciao"}]}`)
	pro := newStub(t, 200, `{"translations":[{"detected_source_language":"DE","text":"Hello"}]}`)
	deeplFreeURL, deeplProURL = free.URL, pro.URL
	ctx := context.Background()
	out, from, err := translateText(ctx, transSettings{provider: "deepl", deeplKey: "abc:fx"}, "Hello", "it")
	if err != nil || out != "Ciao" || from != "en" {
		t.Fatalf("%q %q %v", out, from, err)
	}
	if free.method != "POST" || free.path != "/v2/translate" || free.auth != "DeepL-Auth-Key abc:fx" ||
		free.ctype != "application/json" || free.body != `{"target_lang":"IT","text":["Hello"]}` || pro.n.Load() != 0 {
		t.Errorf("free request: %+v", free)
	}
	// The paid key goes to the paid host; en and pt get a variant.
	if out, _, err = translateText(ctx, transSettings{provider: "deepl", deeplKey: "abc"}, "Hallo", "en"); err != nil || out != "Hello" {
		t.Fatalf("pro: %q %v", out, err)
	}
	if pro.body != `{"target_lang":"EN-US","text":["Hallo"]}` || pro.auth != "DeepL-Auth-Key abc" {
		t.Errorf("pro request: %+v", pro)
	}
	translateText(ctx, transSettings{provider: "deepl", deeplKey: "abc"}, "Hallo", "pt")
	if !strings.Contains(pro.body, `"PT-PT"`) {
		t.Errorf("pt: %s", pro.body)
	}
	before := pro.n.Load()
	for _, c := range []struct {
		key, to string
		want    string
	}{{"", "it", "no_key"}, {"abc", "hi", "unsupported"}} {
		if _, _, err := translateText(ctx, transSettings{provider: "deepl", deeplKey: c.key}, "Hallo", c.to); code(err) != c.want {
			t.Errorf("%+v: %v", c, err)
		}
	}
	if pro.n.Load() != before {
		t.Error("a request went out with no key or no Hindi")
	}
	for status, want := range map[int]string{403: "no_key", 429: "rate_limited", 456: "rate_limited", 400: "unsupported", 503: "translate_failed"} {
		deeplProURL = newStub(t, status, "").URL
		_, _, err := translateText(ctx, transSettings{provider: "deepl", deeplKey: "abc"}, "Hallo", "it")
		if code(err) != want || (status == 503 && !strings.Contains(err.Error(), "503")) {
			t.Errorf("HTTP %d: %v, want %s", status, err, want)
		}
	}
}

func TestTranslateLibre(t *testing.T) {
	s := newStub(t, 200, `{"translatedText":"Ciao","detectedLanguage":{"language":"en","confidence":90}}`)
	ctx := context.Background()
	ts := transSettings{provider: "libre", libreURL: s.URL + "/", libreKey: "k"}
	out, from, err := translateText(ctx, ts, "Hello", "it")
	if err != nil || out != "Ciao" || from != "en" {
		t.Fatalf("%q %q %v", out, from, err)
	}
	if s.method != "POST" || s.path != "/translate" || s.ctype != "application/json" ||
		s.body != `{"api_key":"k","format":"text","q":"Hello","source":"auto","target":"it"}` {
		t.Errorf("request: %+v", s)
	}
	ts.libreKey = ""
	translateText(ctx, ts, "Hello", "it")
	if strings.Contains(s.body, "api_key") {
		t.Errorf("no key set, yet: %s", s.body)
	}
	if _, _, err := translateText(ctx, transSettings{provider: "libre"}, "Hello", "it"); code(err) != "no_key" {
		t.Errorf("no URL: %v", err)
	}
	for status, want := range map[int]string{400: "unsupported", 403: "no_key", 429: "rate_limited", 500: "translate_failed"} {
		_, _, err := translateText(ctx, transSettings{provider: "libre", libreURL: newStub(t, status, "").URL}, "Hello", "it")
		if code(err) != want {
			t.Errorf("HTTP %d: %v, want %s", status, err, want)
		}
	}
}

// No letter: nothing is sent. Already in the target: the translation is empty.
func TestTranslateNoLetterAndSameLanguage(t *testing.T) {
	s := newStub(t, 200, `[[["Hello"]],null,"EN-us"]`)
	defer func(u string) { googleURL = u }(googleURL)
	googleURL = s.URL
	for _, text := range []string{"12 :) !!", "", "\n 👍"} {
		out, from, err := translateText(context.Background(), transSettings{}, text, "it")
		if out != "" || from != "" || err != nil {
			t.Errorf("%q: %q %q %v", text, out, from, err)
		}
	}
	if s.n.Load() != 0 {
		t.Error("a text with no letter was sent")
	}
	out, from, err := translateText(context.Background(), transSettings{}, "Hello", "en-GB")
	if out != "" || from != "EN-us" || err != nil || s.n.Load() != 1 {
		t.Errorf("same language: %q %q %v", out, from, err)
	}
}

func addTexts(st *Store, chat string, rows ...msgRow) {
	done := make(chan bool)
	st.ops <- func(t *storeTx) {
		t.ensureChat(chat, chatUser)
		for _, m := range rows {
			m.Chat = chat
			t.insertMessage(m)
		}
		done <- true
	}
	<-done
}

func trRow(st *Store, chat, id string) (tr, lang, from *string) {
	st.db.QueryRow("SELECT tr, tr_lang, tr_from FROM messages WHERE chat = ? AND id = ?", chat, id).Scan(&tr, &lang, &from)
	return
}

func TestMsgTranslate(t *testing.T) {
	_, st, c := newTestSim(t)
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		q := r.PostForm.Get("q")
		mu.Lock()
		seen = append(seen, q)
		mu.Unlock()
		if q == "boom" {
			w.WriteHeader(429)
			return
		}
		json.NewEncoder(w).Encode([]any{[]any{[]any{"IT:" + q}}, nil, "en"})
	}))
	defer srv.Close()
	defer func(u string) { googleURL = u }(googleURL)
	googleURL = srv.URL
	chat := "x@s.whatsapp.net"
	addTexts(st, chat,
		msgRow{ID: "1", TS: 1, Kind: msgText, Text: "one"},
		msgRow{ID: "2", TS: 2, Kind: msgPhoto, Text: "two", Media: "{}"},
		msgRow{ID: "3", TS: 3, Kind: msgVoice, Text: "three"},
		msgRow{ID: "4", TS: 4, Kind: msgText, Text: "four", Deleted: true},
		msgRow{ID: "5", TS: 5, Kind: msgText, Text: "boom"},
		msgRow{ID: "6", TS: 6, Kind: msgText, Text: "six"})
	ask := func(ids string) (map[string]any, error) {
		r, err := st.handleTranslate([]byte(`{"chat":"` + chat + `","ids":[` + ids + `],"to":"it"}`))
		if err != nil {
			return nil, err
		}
		b, _ := json.Marshal(r)
		var m map[string]any
		json.Unmarshal(b, &m)
		return m, nil
	}

	m, err := ask(`"1","2","3","4","nope"`)
	items := m["items"].([]any)
	if err != nil || len(items) != 2 || items[0].(map[string]any)["id"] != "1" || items[0].(map[string]any)["text"] != "IT:one" ||
		items[1].(map[string]any)["from"] != "en" {
		t.Fatalf("items %v, %v", m, err)
	}
	msgsNamed(t, c, chat, "1")
	if tr, lang, from := trRow(st, chat, "2"); tr == nil || *tr != "IT:two" || *lang != "it" || *from != "en" {
		t.Errorf("row 2 not written: %v %v %v", tr, lang, from)
	}
	if tr, _, _ := trRow(st, chat, "3"); tr != nil {
		t.Error("a voice note was translated")
	}

	// A cache hit asks nobody; another language does.
	seen = nil
	if _, err := ask(`"1","2"`); err != nil || len(seen) != 0 {
		t.Errorf("cache: %v, requests %v", err, seen)
	}
	if _, err := st.handleTranslate([]byte(`{"chat":"` + chat + `","ids":["1"],"to":"de"}`)); err != nil || len(seen) != 1 {
		t.Errorf("another language: %v, requests %v", err, seen)
	}

	// The first failure is the error; rows already written stay.
	if _, err := ask(`"6","5"`); code(err) != "rate_limited" {
		t.Errorf("failure: %v", err)
	}
	if code2 := func() string { _, err := ask(`"3"`); return code(err) }(); code2 != "not_found" {
		t.Errorf("one ineligible id: %s", code2)
	}
	for body, want := range map[string]string{
		`{"ids":["1"]}`:                    "bad_request",
		`{"chat":"` + chat + `","ids":[]}`: "bad_request",
		`{"chat":"` + chat + `","ids":["` + strings.Repeat(`1","`, 200) + `1"]}`: "bad_request",
	} {
		if _, err := st.handleTranslate([]byte(body)); code(err) != want {
			t.Errorf("%.40s: %v, want %s", body, err, want)
		}
	}

	// An edit clears the three columns.
	done := make(chan bool)
	st.ops <- func(tx *storeTx) { tx.editMessage(chat, "1", "uno"); done <- true }
	<-done
	if tr, lang, from := trRow(st, chat, "1"); tr != nil || lang != nil || from != nil {
		t.Errorf("after edit: %v %v %v", tr, lang, from)
	}
}

// No "to": the phone's language, else English.
func TestTranslateDefaultTarget(t *testing.T) {
	_, st, _ := newTestSim(t)
	var tl atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tl.Store(r.URL.Query().Get("tl"))
		io.WriteString(w, `[[["x"]],null,"en"]`)
	}))
	defer srv.Close()
	defer func(u string) { googleURL = u }(googleURL)
	googleURL = srv.URL
	addTexts(st, "x@s.whatsapp.net", msgRow{ID: "1", TS: 1, Kind: msgText, Text: "one"})
	ask := func() {
		if _, err := st.handleTranslate([]byte(`{"chat":"x@s.whatsapp.net","ids":["1"]}`)); err != nil {
			t.Fatal(err)
		}
	}
	ask()
	if tl.Load() != "en" {
		t.Errorf("no language: %v", tl.Load())
	}
	t.Cleanup(func() { setLanguage("") })
	st.nt.setPrefs([]byte(`{"language":"pt_BR"}`))
	ask()
	if tl.Load() != "pt" {
		t.Errorf("phone language: %v", tl.Load())
	}
}

func TestChatSetTranslate(t *testing.T) {
	s, st, c := linkedSim(t)
	anna := simChats[0].jid
	set := func(v string) error {
		_, err := s.handleChatSet([]byte(`{"chat":"` + anna + `","translate":` + v + `}`))
		return err
	}
	if err := set(`"it"`); err != nil {
		t.Fatal(err)
	}
	next(t, c, "chats")
	if n := rows(st, "SELECT count(*) FROM chats WHERE jid = ? AND translate = 'it'", anna); n != 1 {
		t.Error("translate not written")
	}
	if err := set(`""`); err != nil {
		t.Fatal(err)
	}
	eventually(t, st, 1, "SELECT count(*) FROM chats WHERE jid = ? AND translate = ''", anna)
	for _, bad := range []string{`"waytoolong"`, `"it_IT"`, `"i t"`} {
		if code(set(bad)) != "bad_request" {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestPrefsTranslate(t *testing.T) {
	dir := t.TempDir()
	n := newNotifier(dir, waLog.Noop)
	if ts, _ := n.translateSettings(); ts.provider != "google" {
		t.Errorf("default provider %q", ts.provider)
	}
	if _, err := n.setPrefs([]byte(`{"translateProvider":"deepl","deeplKey":"k:fx","libreURL":"http://l","libreKey":"lk"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := n.setPrefs([]byte(`{"translateProvider":"bing"}`)); code(err) != "bad_request" {
		t.Errorf("bad provider: %v", err)
	}
	n.close()
	n = newNotifier(dir, waLog.Noop) // stored
	defer n.close()
	if ts, _ := n.translateSettings(); ts != (transSettings{"deepl", "k:fx", "http://l", "lk"}) {
		t.Errorf("reloaded %+v", ts)
	}
	if _, err := n.setPrefs([]byte(`{"libreKey":""}`)); err != nil {
		t.Errorf("a key cleared: %v", err)
	}
}
