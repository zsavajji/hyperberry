package main

import (
	"strings"
	"sync/atomic"
)

// italian is every word the engine writes into a preview or the Hub, keyed by its English form.
// The strings the UI has too come from hyperberry_it.ts, so both say the same.
var italian = map[string]string{
	"Photo": "Foto", "Video": "Video", "Location": "Posizione", "Contact": "Contatto",
	"Poll": "Sondaggio", "Event": "Evento", "Group invite": "Invito al gruppo",
	"Voice message": "Messaggio vocale", "Audio": "Audio", "Sticker": "Sticker", "Document": "Documento",
	"You": "Tu", "This message was deleted": "Questo messaggio è stato eliminato",
	"New message": "Nuovo messaggio", "Message": "Messaggio",
	"Voice call": "Chiamata vocale", "Video call": "Videochiamata",
	"Incoming voice call": "Chiamata vocale in arrivo", "Missed voice call": "Chiamata vocale persa", "Missed video call": "Videochiamata persa",
	"Pinned a message": "Ha fissato un messaggio", "Unpinned a message": "Ha tolto un messaggio fissato",
	"View once message": "Messaggio visualizzabile una volta", "Message on your phone": "Messaggio sul tuo telefono",
}

// words is the table in use; nil means English, the words themselves. Atomic because the writer
// and the notifier read it while prefs.set changes it.
var words atomic.Pointer[map[string]string]

// setLanguage takes the phone's language, as "it_IT" or "en_US": Italian, or English for any other.
func setLanguage(lang string) {
	if strings.HasPrefix(lang, "it") {
		words.Store(&italian)
	} else {
		words.Store(nil)
	}
}

// word is s in the phone's language.
func word(s string) string {
	if m := words.Load(); m != nil {
		if w, ok := (*m)[s]; ok {
			return w
		}
	}
	return s
}
