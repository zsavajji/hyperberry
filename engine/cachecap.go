package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// What data/cache/ may hold, in MB: voice notes' decoded audio, at 5.5 MB a minute, and photo previews.
// All of it is made again when needed. The user picks the limit in Settings (prefs.set's cacheMB).
const (
	cacheDefaultMB = 64
	cacheMaxMB     = 512
)

// uploadMaxAge is when an upload's temp file is a crash's leftover. Younger, it may be in flight.
const uploadMaxAge = 24 * time.Hour

// pruneCache takes data/cache/ down to limit, the oldest first, never keep (the file just made). Temp files under cache/upload/ count only by age: they go after uploadMaxAge and are otherwise left alone. A file that won't go is logged.
func pruneCache(dir string, limit int64, keep string, now time.Time, log waLog.Logger) {
	root := filepath.Join(dir, "cache")
	upload := filepath.Join(root, "upload") + string(filepath.Separator)
	type entry struct {
		path string
		size int64
		mod  time.Time
	}
	var files []entry
	var total int64
	remove := func(path string) bool {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Warnf("cache: %v", err)
			return false
		}
		return true
	}
	// A missing folder is nothing to do; any other error just ends the walk with what it found.
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if strings.HasPrefix(path, upload) {
			if now.Sub(info.ModTime()) > uploadMaxAge {
				remove(path)
			}
			return nil
		}
		total += info.Size()
		if path != keep {
			files = append(files, entry{path, info.Size(), info.ModTime()})
		}
		return nil
	})
	if total <= limit {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if total <= limit {
			return
		}
		if remove(f.path) {
			total -= f.size
		}
	}
}
