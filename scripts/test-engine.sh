#!/bin/sh
# Runs the engine's tests on the PC, in the build image. The engine is plain Go, so it also
# builds for Linux; only the *_qnx.go files and the phone itself need the BB10 build.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
CACHE=${XDG_CACHE_HOME:-$HOME/.cache}/hyperberry-go   # apart from stock Go's (go-qnx README)
mkdir -p "$CACHE"
docker build -q -t hyperberry-build "$ROOT/docker" >/dev/null
docker run --rm -v "$ROOT:/src" -v "$CACHE:/cache" -w /src/engine \
	-e GOPATH=/cache/gopath -e GOCACHE=/cache/build \
	-e CGO_ENABLED=1 -e CC=cc \
	hyperberry-build sh -euc '
		work=$(sh /src/scripts/hypermeow-work.sh)
		export GOWORK=$work
		go vet ./... && go test ./...'
