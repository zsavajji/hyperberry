package main

// netWatch tells a real network change from netstatus's other events: the default interface or
// its gateway changed, and there is a network to change to. A link coming back after a drop counts,
// so a call's media reconnects at once instead of waiting on meowcaller's 6 s silence watchdog.
type netWatch struct{ last string }

func (w *netWatch) moved(key string) bool {
	changed := key != "" && key != w.last
	w.last = key
	return changed
}
