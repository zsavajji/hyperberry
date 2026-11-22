#!/bin/sh
# Copies rlottie's C API from Tengram's tree into lib/tgs/: libtgs.so (phone-proven, built by Tengram's
# scripts/build-tgs.sh with clang, since rlottie needs C++14) and its two headers. The animated stickers
# (app/src/StickerPlayer.cpp) draw Lottie through it. Not committed. HyperBerry builds no rlottie of its own.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
TENGRAM=$ROOT/../tengram
DIR=$ROOT/lib/tgs
for f in build/prefix/lib/libtgs.so third_party/rlottie/inc/rlottie_capi.h third_party/rlottie/inc/rlottiecommon.h; do
	if [ ! -f "$TENGRAM/$f" ]; then
		echo "fetch-tgs.sh: $TENGRAM/$f is missing: build it with ../tengram/scripts/build-tgs.sh" >&2
		exit 1
	fi
done
mkdir -p "$DIR"
cp "$TENGRAM/build/prefix/lib/libtgs.so" "$TENGRAM/third_party/rlottie/inc/rlottie_capi.h" \
	"$TENGRAM/third_party/rlottie/inc/rlottiecommon.h" "$DIR/"
