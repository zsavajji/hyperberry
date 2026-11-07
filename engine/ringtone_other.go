//go:build !qnx

package main

// startRing has no mm-renderer to play through off BB10, where the engine runs for tests.
func startRing(string) (stop func(), err error) { return func() {}, nil }
