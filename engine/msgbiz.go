package main

import (
	"fmt"
	"strings"

	"github.com/polymorfa/hypermeow/proto/waE2E"
)

// More of flatten (step 6.5): the list, reply, product and order formats that read as text (or a
// photo), and the wrappers that hold an ordinary message.

// moreBiz reads the formats msgflat.go's three don't cover. Replies carry only what was chosen.
func moreBiz(m *waE2E.Message) (bizMsg, bool) {
	switch {
	case m.GetListMessage() != nil:
		l := m.GetListMessage()
		b := bizMsg{ci: l.GetContextInfo(), lines: []string{l.GetTitle(), l.GetDescription(), l.GetFooterText()}}
		for _, s := range l.GetSections() {
			for _, r := range s.GetRows() {
				b.buttons = append(b.buttons, r.GetTitle())
			}
		}
		return b, true
	case m.GetListResponseMessage() != nil:
		r := m.GetListResponseMessage()
		return bizMsg{ci: r.GetContextInfo(), lines: []string{r.GetTitle(), r.GetDescription()}}, true
	case m.GetButtonsResponseMessage() != nil:
		r := m.GetButtonsResponseMessage()
		return bizMsg{ci: r.GetContextInfo(), lines: []string{r.GetSelectedDisplayText()}}, true
	case m.GetTemplateButtonReplyMessage() != nil:
		r := m.GetTemplateButtonReplyMessage()
		return bizMsg{ci: r.GetContextInfo(), lines: []string{r.GetSelectedDisplayText()}}, true
	case m.GetInteractiveResponseMessage() != nil:
		r := m.GetInteractiveResponseMessage()
		return bizMsg{ci: r.GetContextInfo(), lines: []string{r.GetBody().GetText()}}, true
	case m.GetProductMessage() != nil:
		p := m.GetProductMessage()
		s := p.GetProduct()
		return bizMsg{img: s.GetProductImage(), ci: p.GetContextInfo(), lines: []string{s.GetTitle(),
			s.GetDescription(), price(s.GetPriceAmount1000(), s.GetCurrencyCode()), p.GetBody(), p.GetFooter()}}, true
	case m.GetOrderMessage() != nil:
		o := m.GetOrderMessage()
		items := ""
		if n := o.GetItemCount(); n == 1 {
			items = "1 item"
		} else if n > 1 {
			items = fmt.Sprintf("%d items", n)
		}
		return bizMsg{thumb: o.GetThumbnail(), ci: o.GetContextInfo(),
			lines: []string{o.GetOrderTitle(), o.GetMessage(), items}}, true
	}
	return bizMsg{}, false
}

// price words an amount in thousandths as "12.50 EUR"; "" for none.
func price(amount1000 int64, currency string) string {
	if amount1000 == 0 {
		return ""
	}
	cents := (amount1000 + 5) / 10
	return strings.TrimSpace(fmt.Sprintf("%d.%02d %s", cents/100, cents%100, currency))
}

// unwrap turns the messages that hold an ordinary one into it: a round video note is a video, an
// instant photo a photo, and the future-proof wrappers (spoilers, the newer polls, questions) are
// what they wrap. Anything else comes back as it was. ContextInfo stays with the wrapped message,
// so quotes work.
func unwrap(m *waE2E.Message) *waE2E.Message {
	switch {
	case m.GetPtvMessage() != nil:
		return &waE2E.Message{VideoMessage: m.GetPtvMessage()}
	case m.GetInstantImageMessage() != nil:
		return &waE2E.Message{ImageMessage: m.GetInstantImageMessage()}
	}
	for _, w := range []*waE2E.FutureProofMessage{m.GetPollCreationMessageV4(), m.GetSpoilerMessage(),
		m.GetAudioStickerMessage(), m.GetLottieStickerMessage(), m.GetPollCreationOptionImageMessage(), m.GetQuestionMessage(),
		m.GetQuestionReplyMessage(), m.GetBotForwardedMessage()} {
		if in := w.GetMessage(); in != nil {
			return flatten(in)
		}
	}
	return m
}
