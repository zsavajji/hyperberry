package main

import (
	"strconv"
	"syscall"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// memMB is the engine's RLIMIT_DATA in MiB as it started, after raising the soft limit to the hard one; 0 for none
// or unread. The link body carries it: the UI offers the headless-mem addon when it is below 128 (30 is the stock cap).
var memMB int

// readMemMB reads RLIMIT_DATA, raises the soft limit to the hard one when it is lower, logs both, and returns the
// soft limit in MiB (0 for infinity or unreadable). syscall has RLIMIT_DATA, Setrlimit and RLIM_INFINITY on qnx too.
func readMemMB(log waLog.Logger) int {
	var r syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_DATA, &r); err != nil {
		log.Warnf("rlimit DATA: %v", err)
		return 0
	}
	// The mask: the constant is -1 on Linux (the tests) and unsigned on go-qnx.
	infinity := uint64(syscall.RLIM_INFINITY & (1<<64 - 1))
	show := func(v uint64) string {
		if v == infinity {
			return "inf"
		}
		return strconv.FormatUint(v, 10)
	}
	soft, hard := show(r.Cur), show(r.Max)
	raised := ""
	if r.Cur != infinity && (r.Max == infinity || r.Cur < r.Max) {
		up := syscall.Rlimit{Cur: r.Max, Max: r.Max}
		if err := syscall.Setrlimit(syscall.RLIMIT_DATA, &up); err != nil {
			log.Warnf("rlimit DATA: raise: %v", err)
		} else {
			r, raised = up, ", raised to the hard limit"
		}
	}
	log.Infof("rlimit DATA: soft %s, hard %s%s", soft, hard, raised)
	if r.Cur == infinity {
		return 0
	}
	return int(r.Cur >> 20)
}

// goMemLimit is Go's soft memory limit for a cap of memMB: four fifths of it, so the collector works harder well
// before the OS kills us (30 MiB gives 24, 128 gives 102). 24 MiB when the cap is unknown.
func goMemLimit(memMB int) int64 {
	if memMB > 0 {
		return int64(memMB) << 20 / 5 * 4
	}
	return 24 << 20
}
