package main

import "testing"

func TestWordsItalian(t *testing.T) {
	t.Cleanup(func() { setLanguage("") })
	setLanguage("it_IT")
	if got := preview(msgPhoto, "", 0); got != "Foto" {
		t.Errorf("preview %q", got)
	}
	if got := chatPreview(msgRow{Out: true, Kind: msgPhoto}); got != "Tu: Foto" {
		t.Errorf("chatPreview %q", got)
	}
	if got := preview(msgNotice, "pin", 0); got != "Ha fissato un messaggio" {
		t.Errorf("notice %q", got)
	}
	if got := preview(msgOther, unknownText, 0); got != "Messaggio" {
		t.Errorf("unknown %q", got)
	}
	if got := word("not in the table"); got != "not in the table" {
		t.Errorf("unknown word %q", got)
	}
	setLanguage("en_US")
	if got := chatPreview(msgRow{Out: true, Kind: msgPhoto}); got != "You: Photo" {
		t.Errorf("English %q", got)
	}
}

func TestWordsTable(t *testing.T) {
	for _, k := range noticeWords {
		if italian[k] == "" {
			t.Errorf("no Italian for %q", k)
		}
	}
}
