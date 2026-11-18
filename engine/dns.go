package main

import (
	"context"
	"net"
	"sync/atomic"
)

// systemResolver is libsocket's getaddrinfo (netdns=cgo, main.go): the network's own DNS servers.
var systemResolver = net.DefaultResolver

// cloudflareResolver is Go's resolver asking 1.1.1.1 and 1.0.0.1 whatever the network says: it
// dials them in turn, so a retry goes to the other one.
var cloudflareResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, [...]string{"1.1.1.1:53", "1.0.0.1:53"}[cloudflareNext.Add(1)%2])
}}

var cloudflareNext atomic.Uint32

// useCloudflareDNS switches every lookup in the engine (whatsmeow's websocket and media, the
// photos), since they all dial with the default resolver.
// ponytail: a plain pointer swap that a lookup can race; a word on ARMv7, so it sees the old or
// the new resolver. A resolver threaded through each http.Client if the race detector ever matters.
func useCloudflareDNS(on bool) {
	if on {
		net.DefaultResolver = cloudflareResolver
	} else {
		net.DefaultResolver = systemResolver
	}
}
