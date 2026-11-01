//go:build !qnx

package main

// notify and withdraw have no Hub to post to off BB10, where the engine runs for tests.
func notify(itemID, title, body, payload string) error { return nil }

func withdraw(itemID string) error { return nil }
