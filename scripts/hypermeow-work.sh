#!/bin/sh
# Prints the path of a go.work that swaps hypermeow for a copy with engine/framesocket.patch
# applied, and meowcaller for one with engine/meowcaller.patch (`go -overlay` refuses files in
# the module cache). Runs in the build image, from /src/engine. A module whose file has moved on
# makes patch fail, which stops the build instead of losing the fix.
set -eu
go mod download github.com/polymorfa/hypermeow
src=$(go list -m -f '{{.Dir}}' github.com/polymorfa/hypermeow)
rm -rf /tmp/hypermeow
cp -r "$src" /tmp/hypermeow
chmod -R u+w /tmp/hypermeow
patch -s --fuzz=0 -p1 -d /tmp/hypermeow </src/engine/framesocket.patch >&2
go mod download github.com/purpshell/meowcaller
src=$(go list -m -f '{{.Dir}}' github.com/purpshell/meowcaller)
rm -rf /tmp/meowcaller
cp -r "$src" /tmp/meowcaller
chmod -R u+w /tmp/meowcaller
# whatsapp-light's fixes first (engine/meowcaller/*.patch, in order: phase7b.md), then our FFT one.
for p in /src/engine/meowcaller/*.patch; do
	patch -s --fuzz=0 -p1 -d /tmp/meowcaller <"$p" >&2
done
patch -s --fuzz=0 -p1 -d /tmp/meowcaller </src/engine/meowcaller.patch >&2
patch -s --fuzz=0 -p1 -d /tmp/meowcaller </src/engine/meowcaller-group.patch >&2
cat >/tmp/go.work <<EOF
go $(go list -m -f '{{.GoVersion}}')
use /src/engine
replace github.com/polymorfa/hypermeow => /tmp/hypermeow
replace github.com/purpshell/meowcaller => /tmp/meowcaller
EOF
echo /tmp/go.work
