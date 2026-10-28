package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotator(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "x.log")
	r, err := openRotator(path, 256<<10)
	if err != nil {
		t.Fatal(err)
	}
	var last string
	for i := 0; i < 10486; i++ { // ~1 MiB
		last = fmt.Sprintf("%-99d\n", i)
		if _, err := r.Write([]byte(last)); err != nil {
			t.Fatal(err)
		}
	}
	r.Close()
	ents, _ := os.ReadDir(filepath.Dir(path))
	if len(ents) != 2 {
		t.Fatalf("want exactly x.log and x.log.1, got %v", ents)
	}
	for _, p := range []string{path, path + ".1"} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) > 256<<10 {
			t.Errorf("%s is %d bytes, over the limit", p, len(b))
		}
		if p == path && !strings.HasSuffix(string(b), last) {
			t.Errorf("last line is not at the end of the current file")
		}
	}
}

func TestNewLogger(t *testing.T) {
	var b bytes.Buffer
	l := newLogger(&b, "INFO")
	l.Debugf("hidden %d", 1)
	l.Infof("shown %d", 2)
	s := b.String()
	if !strings.Contains(s, "shown 2") {
		t.Errorf("info line missing: %q", s)
	}
	if strings.Contains(s, "hidden") {
		t.Errorf("debug line not dropped at INFO: %q", s)
	}
}
