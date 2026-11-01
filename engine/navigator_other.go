//go:build !qnx

package main

import (
	"os"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

// connectNavigator has no navigator to connect to off BB10, where the engine runs for tests.
func connectNavigator(chan<- os.Signal, func(invocation), waLog.Logger) {}
