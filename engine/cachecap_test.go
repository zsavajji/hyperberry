package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// cacheFile makes a file of n bytes, age old.
func cacheFile(t *testing.T, dir, name string, n int, now time.Time, age time.Duration) string {
	t.Helper()
	p := filepath.Join(dir, "cache", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPruneCacheOldestFirst(t *testing.T) {
	dir, now := t.TempDir(), time.Now()
	old := cacheFile(t, dir, "voice/a.wav", 10, now, 3*time.Hour)
	mid := cacheFile(t, dir, "voice/b.wav", 10, now, 2*time.Hour)
	young := cacheFile(t, dir, "photo/c.jpg", 10, now, time.Hour)
	pruneCache(dir, 20, "", now, waLog.Noop)
	if exists(old) || !exists(mid) || !exists(young) {
		t.Errorf("old %v mid %v young %v", exists(old), exists(mid), exists(young))
	}
}

func TestPruneCacheKeep(t *testing.T) {
	dir, now := t.TempDir(), time.Now()
	oldest := cacheFile(t, dir, "voice/a.wav", 10, now, 3*time.Hour)
	next := cacheFile(t, dir, "voice/b.wav", 10, now, 2*time.Hour)
	pruneCache(dir, 10, oldest, now, waLog.Noop)
	if !exists(oldest) || exists(next) {
		t.Errorf("kept %v, next %v", exists(oldest), exists(next))
	}
}

func TestPruneCacheUnderLimit(t *testing.T) {
	dir, now := t.TempDir(), time.Now()
	a := cacheFile(t, dir, "voice/a.wav", 10, now, time.Hour)
	pruneCache(dir, 10, "", now, waLog.Noop)
	if !exists(a) {
		t.Error("removed under the limit")
	}
}

func TestPruneCacheMissing(t *testing.T) {
	pruneCache(t.TempDir(), 1, "", time.Now(), waLog.Noop) // must not fail
}

func TestPruneCacheUploads(t *testing.T) {
	dir, now := t.TempDir(), time.Now()
	stale := cacheFile(t, dir, "upload/up-1", 100, now, uploadMaxAge+time.Hour)
	fresh := cacheFile(t, dir, "upload/up-2", 100, now, time.Hour)
	voice := cacheFile(t, dir, "voice/a.wav", 10, now, time.Hour)
	// Over the limit by the uploads alone: they don't count, so the voice note stays.
	pruneCache(dir, 10, "", now, waLog.Noop)
	if exists(stale) || !exists(fresh) || !exists(voice) {
		t.Errorf("stale %v fresh %v voice %v", exists(stale), exists(fresh), exists(voice))
	}
	// Over it for real: the young upload is still left alone.
	cacheFile(t, dir, "voice/b.wav", 10, now, 2*time.Hour)
	pruneCache(dir, 10, "", now, waLog.Noop)
	if !exists(fresh) {
		t.Error("a young upload went")
	}
}
