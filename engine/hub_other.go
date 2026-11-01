//go:build !qnx

package main

// udsOpen, udsAccount, udsItem and udsRemove have no Hub to talk to off BB10, where the engine runs for tests.
func udsOpen(string) (fresh bool, err error) { return false, nil }

func udsAccount(bool) error { return nil }

func udsItem(hubItem, bool) error { return nil }

func udsRemove(string) error { return nil }
