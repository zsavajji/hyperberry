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
	"strings"
	"time"
	"unicode"
)

// Message translation (docs/plan/translate.md): one function per service, each translating one text.
// The text leaves WhatsApp's encryption for the service the user picked, so nothing here logs a text or a key.

// Variables so the tests point them at httptest servers.
var (
	googleURL    = "https://translate.googleapis.com/translate_a/single"
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
type transSettings struct{ provider, deeplKey, libreURL, libreKey string }

// translateText translates one text into to. Its result is empty when there is nothing to show: the text has no
// letter (it is never sent) or the service found it already in to. from is the detected language, empty when unknown.
func translateText(ctx context.Context, ts transSettings, text, to string) (out, from string, err error) {
	if strings.IndexFunc(text, unicode.IsLetter) < 0 {
		return "", "", nil
	}
	switch ts.provider {
	case "deepl":
		out, from, err = translateDeepL(ctx, ts, text, to)
	case "libre":
		out, from, err = translateLibre(ctx, ts, text, to)
	default:
		out, from, err = translateGoogle(ctx, text, to)
	}
	if err == nil && langBase(from) == langBase(to) {
		out = ""
	}
	return out, from, err
}

// langBase is the language of a code, without its region: "pt-BR" is "pt".
func langBase(s string) string {
	b, _, _ := strings.Cut(s, "-")
	b, _, _ = strings.Cut(b, "_")
	return strings.ToLower(b)
}

// postTrans posts one request and returns the body of a 200. No answer is offline; a status that is not 200 is the
// code in codes for it, else translate_failed with the status.
func postTrans(ctx context.Context, cli *http.Client, u, ctype string, hdr map[string]string, body []byte, codes map[int]string) ([]byte, error) {
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
		c := codes[resp.StatusCode]
		if c == "" {
			c = "translate_failed"
		}
		return nil, &Error{c, fmt.Sprintf("translate: HTTP %d", resp.StatusCode)}
	}
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
	b, err := postTrans(ctx, googleClient, googleURL+"?"+q.Encode(), "application/x-www-form-urlencoded",
		map[string]string{"User-Agent": "Mozilla/5.0 (Linux; Android 10) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36"},
		[]byte(url.Values{"q": {text}}.Encode()), map[int]string{429: "rate_limited"})
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

func translateDeepL(ctx context.Context, ts transSettings, text, to string) (out, from string, err error) {
	if ts.deeplKey == "" {
		return "", "", &Error{"no_key", "translate: DeepL needs a key"}
	}
	target := strings.ToUpper(to)
	switch langBase(to) {
	case "hi":
		return "", "", &Error{"unsupported", "translate: DeepL has no Hindi"}
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
	b, err := postTrans(ctx, http.DefaultClient, base+"/v2/translate", "application/json", map[string]string{"Authorization": "DeepL-Auth-Key " + ts.deeplKey},
		body, map[int]string{403: "no_key", 429: "rate_limited", 456: "rate_limited", 400: "unsupported"})
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
	b, err := postTrans(ctx, http.DefaultClient, strings.TrimSuffix(ts.libreURL, "/")+"/translate", "application/json", nil,
		body, map[int]string{400: "unsupported", 403: "no_key", 429: "rate_limited"})
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
