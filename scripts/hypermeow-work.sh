#!/bin/sh
# Prints the path of a go.work that swaps hypermeow for a copy with engine/framesocket.patch
# applied (`go -overlay` refuses files in the module cache). Runs in the build image, from
# /src/engine. A hypermeow whose file has moved on makes patch fail, which stops the build
# instead of losing the fix.
set -eu
go mod download github.com/polymorfa/hypermeow
src=$(go list -m -f '{{.Dir}}' github.com/polymorfa/hypermeow)
rm -rf /tmp/hypermeow
cp -r "$src" /tmp/hypermeow
chmod -R u+w /tmp/hypermeow
patch -s --fuzz=0 -p1 -d /tmp/hypermeow </src/engine/framesocket.patch >&2
cat >/tmp/go.work <<EOF
go $(go list -m -f '{{.GoVersion}}')
use /src/engine
replace github.com/polymorfa/hypermeow => /tmp/hypermeow
EOF
echo /tmp/go.work
