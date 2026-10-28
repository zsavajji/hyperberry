#!/bin/sh
# Builds build/hyperberry.bar: the engine (scripts/build-engine.sh), then the Cascades UI in the
# Native SDK 10.3.1 image, packaged unsigned with both binaries. Each package gets the next
# build id from build/buildid.txt, so the installer never keeps an older app.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
IMAGE=${BB10_NDK_IMAGE:-yamsergey/bb10-ndk:0.6}
"$ROOT/scripts/build-engine.sh" >/dev/null
[ -f "$ROOT/build/buildid.txt" ] || echo 1 >"$ROOT/build/buildid.txt"
docker run --rm -v "$ROOT:/src" -w /src "$IMAGE" bash -c '
	source /root/bbndk/bbndk-env_10_3_1_995.sh >/dev/null
	set -eu
	mkdir -p build/ui
	(cd build/ui && qmake -spec blackberry-armv7le-qcc CONFIG+=device ../../app/hyperberry.pro >/dev/null &&
		make -j"$(nproc)" >/dev/null)
	cd app && blackberry-nativepackager -package ../build/hyperberry.bar -buildIdFile ../build/buildid.txt \
		bar-descriptor.xml -configuration Device-Release
	rm -f assets/.assets.index
'
unzip -p "$ROOT/build/hyperberry.bar" META-INF/MANIFEST.MF | grep -E '^Package-(Id|Version):'
