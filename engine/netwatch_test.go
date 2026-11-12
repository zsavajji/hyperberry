package main

import "testing"

func TestNetWatch(t *testing.T) {
	var w netWatch
	for _, c := range []struct {
		key  string
		want bool
	}{
		{"", false},
		{"wifi|1", true}, // the first network: harmless, there is no call yet
		{"wifi|1", false},
		{"", false},
		{"wifi|1", true}, // back after a drop
		{"cell|2", true},
	} {
		if got := w.moved(c.key); got != c.want {
			t.Errorf("moved(%q) = %v; want %v", c.key, got, c.want)
		}
	}
}
