package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	waLog "github.com/polymorfa/hypermeow/util/log"
	"github.com/rs/zerolog"
)

// logMax is the size of data/logs/hyperberryd.log; with its one older copy the phone keeps
// at most twice this.
const logMax = 256 << 10

// rotator keeps a log file under max bytes with one older copy: when a write would cross max,
// path is renamed to path+".1" (replacing the previous one) and a new file starts.
type rotator struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

func openRotator(path string, max int64) (*rotator, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	r := &rotator{path: path, max: max}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotator) open() error {
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

func (r *rotator) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size > 0 && r.size+int64(len(p)) > r.max {
		r.f.Close()
		if err := os.Rename(r.path, r.path+".1"); err != nil {
			return 0, err
		}
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}

// newLogger gives hypermeow's logger interface, writing readable lines to w.
func newLogger(w io.Writer, level string) waLog.Logger {
	lvl, err := zerolog.ParseLevel(strings.ToLower(level))
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zl := zerolog.New(zerolog.ConsoleWriter{Out: w, NoColor: true, TimeFormat: "2006-01-02 15:04:05.000"}).
		Level(lvl).With().Timestamp().Logger()
	return waLog.Zerolog(zl)
}
