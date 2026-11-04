#!/bin/sh
# Builds build/hyperberryd for BlackBerry 10 (qnx/arm, cgo through bb10-clang), then checks that
# every symbol it imports exists in the phone's libraries: a missing one links fine here and
# only fails on the phone.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
CACHE=${XDG_CACHE_HOME:-$HOME/.cache}/hyperberry-go   # apart from stock Go's (go-qnx README)
mkdir -p "$CACHE" "$ROOT/build"
docker build -q -t hyperberry-build "$ROOT/docker" >/dev/null
docker run --rm -v "$ROOT:/src" -v "$CACHE:/cache" -w /src/engine \
	-e GOPATH=/cache/gopath -e GOCACHE=/cache/build \
	-e CGO_ENABLED=1 -e CC=bb10-clang -e GOOS=qnx -e GOARCH=arm -e GOARM=7 \
	hyperberry-build sh -euc '
		# libunifieddatasourcec.so NEEDs the Qt libraries: only the linker looks for them here.
		export CGO_LDFLAGS="-Wl,-rpath-link,$QNX_TARGET/armle-v7/usr/lib/qt4/lib"
		work=$(sh /src/scripts/hypermeow-work.sh)
		export GOWORK=$work
		go build -trimpath -ldflags="-s -w" -o /src/build/hyperberryd .
		L=$QNX_TARGET/armle-v7/lib
		for l in $L/libc.so.3 $L/libm.so.2 $L/libsocket.so.3 $L/../usr/lib/libbps.so.3 \
				$L/../usr/lib/libunifieddatasourcec.so.1; do
			nm -D --defined-only $l | awk "{ print \$3 }"
		done | sed "s/@.*//" | sort -u >/tmp/defined
		missing=$(nm -D --undefined-only /src/build/hyperberryd | awk "\$1 == \"U\" { print \$2 }" |
			sed "s/@.*//" | sort -u | comm -23 - /tmp/defined)
		[ -z "$missing" ] || { echo "not on BB10: $missing"; exit 1; }
		ls -l /src/build/hyperberryd'
