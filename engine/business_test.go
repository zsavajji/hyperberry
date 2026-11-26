package main

import (
	"testing"

	"github.com/polymorfa/hypermeow/proto/waVnameCert"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"
)

func TestBusinessFrom(t *testing.T) {
	vn := &types.VerifiedName{Details: &waVnameCert.VerifiedNameCertificate_Details{VerifiedName: proto.String("The Drunken Duck")}}
	b := businessFrom(vn, &types.BusinessProfile{
		Categories:    []types.Category{{Name: "Ristorante"}, {Name: ""}, {Name: "Bar"}},
		BusinessHours: []types.BusinessHoursConfig{{DayOfWeek: "mon", Mode: "specific_hours", OpenTime: "540", CloseTime: "1080"}, {DayOfWeek: "xyz"}, {DayOfWeek: "sun", Mode: "open_24h"}},
	})
	if b.Name != "The Drunken Duck" || b.Category != "Ristorante, Bar" {
		t.Errorf("name %q category %q", b.Name, b.Category)
	}
	if len(b.Hours) != 2 || b.Hours[0] != (bizHour{1, "specific_hours", 540, 1080}) || b.Hours[1] != (bizHour{0, "open_24h", 0, 0}) {
		t.Errorf("hours %+v", b.Hours)
	}
	if b := businessFrom(vn, nil); b.Name != "The Drunken Duck" || b.Hours == nil || b.Websites == nil {
		t.Errorf("no profile: %+v", b)
	}
}
