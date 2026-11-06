package main

import (
	"errors"
	"strings"
	"testing"
)

const samplePPS = `@status
Status:json:{"x":1}
BatteryInfo:json:{"BatteryState":"READY","NumOfBattery":1,"ActiveBattery":0,"BatteryStatus":[{"StateOfCharge":87,"Temperature":30}]}
Other::1
`

func TestBatteryLevel(t *testing.T) {
	if l, ok := batteryLevel([]byte(samplePPS)); !ok || l != 87 {
		t.Errorf("got %d %v", l, ok)
	}
	for name, in := range map[string]string{
		"no line":  "@status\nStatus:json:{}\n",
		"bad json": "@status\nBatteryInfo:json:{nope\n",
		"empty":    "@status\nBatteryInfo:json:{\"BatteryStatus\":[]}\n",
	} {
		if _, ok := batteryLevel([]byte(in)); ok {
			t.Errorf("%s: ok", name)
		}
	}
}

func TestStatsLine(t *testing.T) {
	if l := statsLine([]byte(samplePPS), nil); !strings.HasPrefix(l, "stats: heap ") || !strings.HasSuffix(l, "battery 87%") {
		t.Error(l)
	}
	if l := statsLine(nil, errors.New("x")); !strings.HasSuffix(l, "battery ?") {
		t.Error(l)
	}
}
