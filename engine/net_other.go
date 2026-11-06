//go:build !qnx

package main

// cellular is never true off BB10, where the engine runs for tests.
func cellular() bool { return false }
