package main

import (
	"strconv"
	"strings"

	"github.com/polymorfa/hypermeow/types"
)

// businessInfo is contact.info's business block: what the phone's contact page shows for a
// WhatsApp Business account. Hours as the business set them; the UI works out "open now".
type businessInfo struct {
	Name        string    `json:"name"`     // the verified name, "" when the certificate has none
	Category    string    `json:"category"` // the categories, joined
	Description string    `json:"description"`
	Address     string    `json:"address"`
	Email       string    `json:"email"`
	Websites    []string  `json:"websites"`
	Hours       []bizHour `json:"hours"` // none when the business set no hours
}

// bizHour is one business_hours_config: a day (0 Sunday … 6 Saturday), its mode (open_24h,
// specific_hours, appointment_only) and, for specific_hours, minutes from midnight. A day may
// have several; a day with none is closed.
type bizHour struct {
	Day   int    `json:"day"`
	Mode  string `json:"mode"`
	Open  int    `json:"open"`
	Close int    `json:"close"`
}

var bizDays = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

// businessFrom builds the block from the verified name and the profile, either of which may be nil.
func businessFrom(vn *types.VerifiedName, p *types.BusinessProfile) *businessInfo {
	b := &businessInfo{Websites: []string{}, Hours: []bizHour{}}
	if vn != nil {
		b.Name = vn.Details.GetVerifiedName()
	}
	if p == nil {
		return b
	}
	b.Description, b.Address, b.Email = p.Description, p.Address, p.Email
	b.Websites = append(b.Websites, p.Websites...)
	var cats []string
	for _, c := range p.Categories {
		if c.Name != "" {
			cats = append(cats, c.Name)
		}
	}
	b.Category = strings.Join(cats, ", ")
	for _, h := range p.BusinessHours {
		d, ok := bizDays[strings.ToLower(h.DayOfWeek)]
		if !ok {
			continue
		}
		open, _ := strconv.Atoi(h.OpenTime)
		close, _ := strconv.Atoi(h.CloseTime)
		b.Hours = append(b.Hours, bizHour{Day: d, Mode: h.Mode, Open: open, Close: close})
	}
	return b
}
