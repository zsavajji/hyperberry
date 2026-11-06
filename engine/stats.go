package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// Phase 8's overnight numbers come from the phone itself: plugged in, it charges, so a line an hour says what the engine holds and what the battery has left.
const statsEvery = time.Hour

const batteryStatus = "/pps/system/BattMgr/status"

// logStats logs the line every statsEvery, for the life of the process.
func logStats(log waLog.Logger) {
	for {
		log.Infof("%s", statsLine(os.ReadFile(batteryStatus)))
		time.Sleep(statsEvery)
	}
}

// statsLine is `stats: heap 1 MB, from the OS 5 MB, 12 goroutines, battery 87%`, or `battery ?` when the file can't be read.
func statsLine(pps []byte, err error) string {
	battery := "?"
	if err == nil {
		if level, ok := batteryLevel(pps); ok {
			battery = fmt.Sprintf("%d%%", level)
		}
	}
	return fmt.Sprintf("stats: %s, %d goroutines, battery %s", memUsage(), runtime.NumGoroutine(), battery)
}

// batteryLevel reads the percentage from the PPS object: a line `BatteryInfo:json:{"BatteryStatus":[{"StateOfCharge":87,...}],...}`. ok is false when there's no such line, or it doesn't parse.
func batteryLevel(pps []byte) (level int, ok bool) {
	_, js, found := bytes.Cut(pps, []byte("BatteryInfo:json:"))
	if !found {
		return 0, false
	}
	if i := bytes.IndexByte(js, '\n'); i >= 0 {
		js = js[:i]
	}
	var info struct {
		BatteryStatus []struct{ StateOfCharge int }
	}
	if json.Unmarshal(js, &info) != nil || len(info.BatteryStatus) == 0 {
		return 0, false
	}
	return info.BatteryStatus[0].StateOfCharge, true
}
