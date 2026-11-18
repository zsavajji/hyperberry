package main

import "testing"

func TestLookupPhone(t *testing.T) {
	for in, want := range map[string]string{
		`{"phone":"391234567890"}`:     "391234567890",
		`{"phone":"+39 123 456-7890"}`: "391234567890",
		`{"phone":"12345"}`:            "",
		`{"phone":"1234567890123456"}`: "",
		`{}`:                           "",
	} {
		got, err := lookupPhone([]byte(in))
		if got != want || (want == "") != (err != nil) {
			t.Errorf("%s: %q, %v; want %q", in, got, err, want)
		}
	}
}
