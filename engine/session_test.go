package main

import (
	"encoding/json"
	"errors"
	"testing"
)

// A number PairPhone would refuse is refused before the session is touched, so it doesn't cost
// the running QR code. The Session is empty: reaching it would panic.
func TestCodeRefusesBadNumbers(t *testing.T) {
	for _, phone := range []string{"12", "000000", "123456", "0612345678", "0033", "+0 333 1234567"} {
		req, _ := json.Marshal(map[string]string{"phone": phone})
		_, err := (&Session{}).handleCode(req)
		var e *Error
		if !errors.As(err, &e) || e.Code != "bad_phone" {
			t.Errorf("%q: got %v, want bad_phone", phone, err)
		}
	}
}
