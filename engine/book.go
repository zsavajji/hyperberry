package main

import (
	"encoding/json"
	"maps"
	"os"
	"sort"
	"strings"
	"sync"
)

// phoneBook is the BB10's own address book: the UI reads it (only the UI can) and hands it over
// with contacts.book. Kept on disk because the engine restarts headless, without the UI, and would
// otherwise rename everyone back until the UI next opens.
var phoneBook = &addressBook{}

type addressBook struct {
	mu       sync.RWMutex
	path     string
	byNumber map[string]string // bookDigits of the number -> name
}

// bookEntry is one number of one contact, as the UI sends it.
type bookEntry struct{ Number, Name string }

// bookDigits is the digits of a phone number as typed, without the international prefix: "+39 333
// 123 4567" and "0039 3331234567" both give 393331234567. A national number keeps its own leading
// zero ("041 1234567").
func bookDigits(s string) string {
	d := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
	return strings.TrimPrefix(d, "00")
}

// twoDigitCodes are E.164's country codes of two digits. Of the rest, 1 and 7 have one digit and
// the others three.
const twoDigitCodes = " 20 27 30 31 32 33 34 36 39 40 41 43 44 45 46 47 48 49 51 52 53 54 55 56 57 58 60 61 62 63 64 65 66 81 82 84 86 90 91 92 93 94 95 98 "

// callingCode is the country code of an international number (digits only, as WhatsApp's user part
// has it), "" when the number is too short to have one.
func callingCode(number string) string {
	n := 3
	switch {
	case strings.HasPrefix(number, "1"), strings.HasPrefix(number, "7"):
		n = 1
	case len(number) >= 2 && strings.Contains(twoDigitCodes, " "+number[:2]+" "):
		n = 2
	}
	if len(number) < n {
		return ""
	}
	return number[:n]
}

// name is the book's name for a phone JID's user part, "" for none; own is our number. A number
// saved without a country code ("333 1234567", or "07700 900123" with the UK's trunk zero) matches
// only inside our own country.
// ponytail: the country is the linked account's, not the Q10 SIM's; a traveller's national
// numbers still match their home country, and a book kept in two countries misses one of them.
// Read the SIM's country in the UI and send it if that shows.
func (b *addressBook) name(user, own string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if n := b.byNumber[user]; n != "" {
		return n
	}
	cc := callingCode(own)
	if cc == "" || !strings.HasPrefix(user, cc) {
		return ""
	}
	tail := user[len(cc):]
	if n := b.byNumber[tail]; n != "" {
		return n
	}
	return b.byNumber["0"+tail]
}

// load remembers where the book lives and reads it; a missing or damaged file is an empty book.
func (b *addressBook) load(path string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.path, b.byNumber = path, nil
	var m map[string]string
	if raw, err := os.ReadFile(path); err == nil && json.Unmarshal(raw, &m) == nil {
		b.byNumber = m
	}
}

// set replaces the book and keeps it on disk. It reports whether anything changed: the UI sends
// the book on every connect. A number of fewer than 6 digits is a short code, not a person; if
// two contacts share a number the first one counts.
func (b *addressBook) set(entries []bookEntry) (changed bool) {
	m := make(map[string]string, len(entries))
	for _, e := range entries {
		d, name := bookDigits(e.Number), strings.TrimSpace(e.Name)
		if name == "" || len(d) < 6 || m[d] != "" {
			continue
		}
		m[d] = name
	}
	b.mu.Lock() // held through the write, so two sets can't land on disk out of order
	defer b.mu.Unlock()
	if maps.Equal(m, b.byNumber) {
		return false
	}
	b.byNumber = m
	if b.path != "" {
		// A failed write is ignored: the names are in memory, and only a headless restart before
		// the UI next sends the book would lose them. There is no log for a book to go to.
		if raw, err := json.Marshal(m); err == nil {
			tmp := b.path + ".tmp"
			if os.WriteFile(tmp, raw, 0o600) == nil && os.Rename(tmp, b.path) != nil {
				os.Remove(tmp)
			}
		}
	}
	return true
}

// candidates is every full number (country code first, digits only) the book's entries may be, each
// with the entry's name; own is our number, for our country's code. The book keeps digits only, so
// whether one was typed with its country code is a guess, made as name() matches them: a leading 0
// is national, and taken with and without it (Italy keeps its 0, the UK drops it); 11 digits or more
// is already international; anything shorter is national. A wrong guess only asks WhatsApp about a
// number nobody has, as long as it isn't someone else's: hence not trying every form of every entry.
// ponytail: the country is the linked account's, as in name().
func (b *addressBook) candidates(own string) map[string]string {
	cc := callingCode(own)
	b.mu.RLock()
	defer b.mu.RUnlock()
	keys := make([]string, 0, len(b.byNumber))
	for d := range b.byNumber {
		keys = append(keys, d)
	}
	sort.Strings(keys) // so a number two entries share goes to the same one every time
	out := map[string]string{}
	add := func(num, name string) {
		if _, ok := out[num]; !ok && num != own {
			out[num] = name
		}
	}
	for _, d := range keys {
		name := b.byNumber[d]
		switch {
		case strings.HasPrefix(d, "0"):
			if cc != "" {
				add(cc+d[1:], name)
				add(cc+d, name)
			}
		case len(d) >= 11:
			add(d, name)
		case cc != "":
			add(cc+d, name)
		}
	}
	return out
}
