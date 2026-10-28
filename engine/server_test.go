package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

type conn struct {
	net.Conn
	r *bufio.Reader
}

type testServer struct {
	*Server
	dir, token string
	port       int
	served     chan error
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	dir := t.TempDir()
	s, err := NewServer(dir, waLog.Noop, func() any { return map[string]any{"me": "x"} })
	if err != nil {
		t.Fatal(err)
	}
	s.Handle("echo", func(req json.RawMessage) (any, error) {
		var r struct{ X any }
		json.Unmarshal(req, &r)
		return r.X, nil
	})
	s.Handle("busy", func(json.RawMessage) (any, error) { return nil, &Error{Code: "busy", Message: "later"} })
	s.Handle("boom", func(json.RawMessage) (any, error) { return nil, errors.New("boom") })
	ts := &testServer{Server: s, dir: dir, served: make(chan error, 1)}
	go func() { ts.served <- s.Serve() }()
	b, err := os.ReadFile(filepath.Join(dir, "engine.json"))
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Port  int
		Token string
	}
	if err := json.Unmarshal(b, &info); err != nil {
		t.Fatal(err)
	}
	ts.port, ts.token = info.Port, info.Token
	t.Cleanup(func() { s.Close() })
	return ts
}

func (ts *testServer) dial(t *testing.T) *conn {
	t.Helper()
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ts.port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &conn{c, bufio.NewReader(c)}
}

func (c *conn) send(t *testing.T, line string) {
	t.Helper()
	if _, err := io.WriteString(c, line+"\n"); err != nil {
		t.Fatal(err)
	}
}

func (c *conn) read(t *testing.T) map[string]any {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		t.Fatal("read:", err)
	}
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("bad line %q: %v", line, err)
	}
	return m
}

func (c *conn) wantEOF(t *testing.T, name string) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.r.ReadByte(); err != io.EOF {
		t.Errorf("%s: want EOF, got %v", name, err)
	}
}

func errCode(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

func (ts *testServer) login(t *testing.T) *conn {
	t.Helper()
	c := ts.dial(t)
	c.send(t, fmt.Sprintf(`{"@":"h","cmd":"hello","token":%q,"proto":1}`, ts.token))
	if m := c.read(t); m["ok"] != true {
		t.Fatalf("login: %v", m)
	}
	return c
}

func TestEngineJSON(t *testing.T) {
	ts := newTestServer(t)
	st, err := os.Stat(filepath.Join(ts.dir, "engine.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("engine.json mode: %v", st.Mode().Perm())
	}
	if ts.port == 0 || len(ts.token) != 32 {
		t.Errorf("engine.json content: port %d, token %q", ts.port, ts.token)
	}
}

func TestHello(t *testing.T) {
	ts := newTestServer(t)
	c := ts.dial(t)
	c.send(t, fmt.Sprintf(`{"@":"7","cmd":"hello","token":%q,"proto":1}`, ts.token))
	m := c.read(t)
	if m["@"] != "7" || m["ok"] != true || !reflect.DeepEqual(m["result"], map[string]any{"me": "x"}) {
		t.Errorf("good hello: %v", m)
	}
}

func TestRefusals(t *testing.T) {
	ts := newTestServer(t)
	c := ts.dial(t)
	c.send(t, `{"@":"1","cmd":"hello","token":"nope","proto":1}`)
	if m := c.read(t); m["@"] != "1" || errCode(m) != "auth" {
		t.Errorf("wrong token: %v", m)
	}
	c.wantEOF(t, "wrong token")

	c = ts.dial(t)
	c.send(t, `{"@":"2","cmd":"echo","x":1}`)
	if m := c.read(t); errCode(m) != "auth" {
		t.Errorf("non-hello first: %v", m)
	}
	c.wantEOF(t, "non-hello first")

	c = ts.dial(t)
	c.send(t, fmt.Sprintf(`{"@":"3","cmd":"hello","token":%q,"proto":2}`, ts.token))
	if m := c.read(t); errCode(m) != "proto" {
		t.Errorf("wrong proto: %v", m)
	}
	c.wantEOF(t, "wrong proto")
}

func TestCommands(t *testing.T) {
	ts := newTestServer(t)
	c := ts.login(t)
	for _, tc := range []struct{ name, line, code string }{
		{"echo", `{"@":"a","cmd":"echo","x":"hi"}`, ""},
		{"unknown cmd", `{"@":"b","cmd":"nope"}`, "unknown_cmd"},
		{"Error code kept", `{"@":"c","cmd":"busy"}`, "busy"},
		{"plain error", `{"@":"d","cmd":"boom"}`, "failed"},
		{"no cmd", `{"@":"e"}`, "bad_request"},
		{"not an object", `[1]`, "bad_request"},
	} {
		c.send(t, tc.line)
		m := c.read(t)
		if errCode(m) != tc.code {
			t.Errorf("%s: %v", tc.name, m)
		}
		if tc.name == "echo" && (m["ok"] != true || m["result"] != "hi" || m["@"] != "a") {
			t.Errorf("echo: %v", m)
		}
	}
}

func TestEmit(t *testing.T) {
	ts := newTestServer(t)
	a, b := ts.login(t), ts.login(t)
	idle := ts.dial(t)
	ts.Emit("chats", map[string]any{"jids": []string{"j"}})
	for name, c := range map[string]*conn{"client a": a, "client b": b} {
		m := c.read(t)
		if m["ev"] != "chats" || m["jids"] == nil {
			t.Errorf("%s: %v", name, m)
		}
	}
	ts.Emit("conn", nil)
	idle.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := idle.r.ReadByte(); err == nil {
		t.Error("unauthenticated client received an event")
	} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Errorf("unauthenticated client: want timeout, got %v", err)
	}
}

func TestClose(t *testing.T) {
	ts := newTestServer(t)
	c := ts.login(t)
	if err := ts.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ts.dir, "engine.json")); !os.IsNotExist(err) {
		t.Errorf("engine.json after Close: %v", err)
	}
	select {
	case err := <-ts.served:
		if err != nil {
			t.Errorf("Serve returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return")
	}
	c.wantEOF(t, "client after Close")
}
