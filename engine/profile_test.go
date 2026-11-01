package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// jpegFile writes a file that starts like a JPEG and returns its path.
func jpegFile(t *testing.T, name string, size int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	data := bytes.Repeat([]byte{7}, size)
	copy(data, []byte{0xFF, 0xD8, 0xFF})
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProfileSetValidation(t *testing.T) {
	txt := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(txt, []byte("not a picture"), 0o644)
	for name, req := range map[string]string{
		"nothing":      `{}`,
		"not json":     `[1]`,
		"empty name":   `{"name":"  "}`,
		"long name":    `{"name":"` + strings.Repeat("x", 26) + `"}`,
		"long about":   `{"about":"` + strings.Repeat("é", 140) + `"}`,
		"relative":     `{"picture":"me.jpg"}`,
		"missing":      `{"picture":"/no/such/file.jpg"}`,
		"not a JPEG":   `{"picture":"` + txt + `"}`,
		"over 1 MB":    `{"picture":"` + jpegFile(t, "big.jpg", maxProfilePhoto+1) + `"}`,
		"good + a bad": `{"name":"Ann","picture":"/no/such/file.jpg"}`,
	} {
		_, err := parseProfileSet([]byte(req))
		if e, ok := err.(*Error); !ok || e.Code != "bad_request" {
			t.Errorf("%s: %v, want bad_request", name, err)
		}
	}
	// The limits themselves are fine, counted in characters not bytes.
	ok := `{"name":"` + strings.Repeat("é", 25) + `","about":"` + strings.Repeat("é", 139) + `","picture":"` +
		jpegFile(t, "max.jpg", maxProfilePhoto) + `"}`
	if _, err := parseProfileSet([]byte(ok)); err != nil {
		t.Errorf("at the limits: %v", err)
	}
	if r, err := parseProfileSet([]byte(`{"about":""}`)); err != nil || r.about == nil || *r.about != "" {
		t.Errorf("an empty about clears it: %+v %v", r, err)
	}
}

func TestSimProfile(t *testing.T) {
	s, _, _ := linkedSim(t)
	get := func() profileInfo {
		t.Helper()
		r, err := s.handleProfileGet(nil)
		if err != nil {
			t.Fatal(err)
		}
		return r.(profileInfo)
	}
	if p := get(); p.Name != "Sim" || p.About != "Available" || p.Phone != "+390000000000" || p.Picture != "" {
		t.Fatalf("at the start: %+v", p)
	}
	set := func(req string) error { _, err := s.handleProfileSet([]byte(req)); return err }

	if err := set(`{"name":" Anna B ","about":"Out 🍕"}`); err != nil {
		t.Fatal(err)
	}
	if p := get(); p.Name != "Anna B" || p.About != "Out 🍕" || p.Picture != "" {
		t.Errorf("after name and about: %+v", p)
	}
	if err := set(`{"name":"Sim"}`); err != nil || get().About != "Out 🍕" {
		t.Errorf("a name alone leaves the about: %v %+v", err, get())
	}

	src := jpegFile(t, "crop.jpg", 2000)
	if err := set(`{"picture":"` + src + `"}`); err != nil {
		t.Fatal(err)
	}
	p := get()
	want := filepath.Join(s.srv.dir, "profile", "me.jpg")
	if abs, _ := filepath.Abs(want); p.Picture != abs {
		t.Fatalf("picture %q, want %q", p.Picture, abs)
	}
	if a, _ := os.ReadFile(src); !bytes.Equal(a, mustRead(t, p.Picture)) {
		t.Error("me.jpg isn't a copy of the picture")
	}
	if err := set(`{"picture":""}`); err != nil {
		t.Fatal(err)
	}
	if p := get(); p.Picture != "" {
		t.Errorf("after removing: %+v", p)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Errorf("me.jpg is still there: %v", err)
	}
	if err := set(`{"picture":""}`); err != nil {
		t.Errorf("removing no picture: %v", err)
	}

	for _, bad := range []string{`{}`, `{"name":""}`, `{"about":"` + strings.Repeat("x", 140) + `"}`, `{"picture":"nope"}`} {
		if err := set(bad); err == nil {
			t.Errorf("%s: want bad_request", bad)
		}
	}
	if p := get(); p.Name != "Sim" {
		t.Errorf("a refused set changed something: %+v", p)
	}
}

func TestSimProfileUnlinked(t *testing.T) {
	s, _, _ := newTestSim(t)
	if _, err := s.handleProfileGet(nil); err == nil {
		t.Error("profile.get while unlinked: want an error")
	}
	if _, err := s.handleProfileSet([]byte(`{"name":"A"}`)); err == nil {
		t.Error("profile.set while unlinked: want an error")
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestUnlinkWipesProfile(t *testing.T) {
	s, _, _ := linkedSim(t)
	if _, err := s.handleProfileSet([]byte(`{"picture":"` + jpegFile(t, "p.jpg", 100) + `"}`)); err != nil {
		t.Fatal(err)
	}
	s.handleRemoved(nil)
	dir := filepath.Join(s.srv.dir, "profile")
	for end := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return
		}
		if time.Now().After(end) {
			t.Fatal("data/profile is still there after unlinking")
		}
	}
}
