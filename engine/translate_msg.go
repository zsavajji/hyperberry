package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
)

const (
	translateAtOnce = 4   // requests in flight for one msg.translate
	translateMaxIDs = 200 // ids in one msg.translate
)

type transItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	From string `json:"from"`
}

// handleTranslate is msg.translate for the real and the simulated account: it reads and writes chats.db through
// the store and asks the user's service for what isn't translated yet.
func (st *Store) handleTranslate(req json.RawMessage) (any, error) {
	var r struct {
		Chat string
		IDs  []string
		To   string
	}
	if json.Unmarshal(req, &r) != nil || r.Chat == "" || len(r.IDs) == 0 || len(r.IDs) > translateMaxIDs {
		return nil, &Error{"bad_request", "msg.translate needs chat, 1 to 200 ids and to"}
	}
	ts, lang := st.nt.translateSettings()
	to := strings.TrimSpace(r.To)
	if to == "" {
		to = langBase(lang)
	}
	if to == "" {
		to = "en"
	}

	type row struct {
		id, text, tr, trLang, trFrom string
		cached                       bool
	}
	got := make(chan []row, 1)
	st.ops <- func(t *storeTx) {
		var rows []row
		for _, id := range r.IDs {
			var x row
			var kind int
			var deleted bool
			var text, tr, trLang, trFrom *string
			if t.tx.QueryRow("SELECT kind, text, deleted, tr, tr_lang, tr_from FROM messages WHERE chat = ? AND id = ?", r.Chat, id).
				Scan(&kind, &text, &deleted, &tr, &trLang, &trFrom) != nil ||
				deleted || text == nil || *text == "" || (kind != msgText && kind != msgPhoto && kind != msgVideo) {
				continue
			}
			x.id, x.text = id, *text
			if trLang != nil && *trLang == to && tr != nil {
				x.cached, x.tr = true, *tr
				if trFrom != nil {
					x.trFrom = *trFrom
				}
			}
			rows = append(rows, x)
		}
		got <- rows
	}
	rows := <-got
	if len(rows) == 0 && len(r.IDs) == 1 {
		return nil, &Error{"not_found", "no such text"}
	}

	items := make([]transItem, len(rows))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var first error
	var wg sync.WaitGroup
	sem := make(chan struct{}, translateAtOnce)
	for i, x := range rows {
		items[i] = transItem{ID: x.id, Text: x.tr, From: x.trFrom}
		if x.cached {
			continue
		}
		sem <- struct{}{}
		if ctx.Err() != nil {
			<-sem
			break
		}
		wg.Add(1)
		go func(i int, x row) {
			defer wg.Done()
			defer func() { <-sem }()
			out, from, err := translateText(ctx, ts, x.text, to)
			if err != nil {
				mu.Lock()
				if first == nil {
					first = err
					cancel()
				}
				mu.Unlock()
				return
			}
			items[i].Text, items[i].From = out, from
			// AND text: an edit that landed meanwhile keeps its cleared columns, and the UI asks again.
			st.ops <- func(t *storeTx) {
				if t.exec("UPDATE messages SET tr = ?, tr_lang = ?, tr_from = ? WHERE chat = ? AND id = ? AND text = ?", out, to, from, r.Chat, x.id, x.text) > 0 {
					t.changed(r.Chat, x.id)
				}
			}
		}(i, x)
	}
	wg.Wait()
	if first != nil {
		return nil, first
	}
	return map[string]any{"items": items}, nil
}
