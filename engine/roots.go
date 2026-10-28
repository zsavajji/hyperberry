// BB10 has no CA certificates where Go looks for them (go-qnx's root_qnx.go reads
// /etc/openssl, which BB10 doesn't have), so TLS to WhatsApp failed with "certificate signed
// by unknown authority" on the Q10 (2026-10-28). The engine carries Mozilla's roots instead,
// and uses them even on a phone with a store of its own, so every phone trusts the same set.
// They age with golang.org/x/crypto: bump it with hypermeow.

//go:debug x509usefallbackroots=1

package main

import _ "golang.org/x/crypto/x509roots/fallback"
