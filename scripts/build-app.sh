#!/bin/sh
# Builds build/hyperberry.bar: the engine (scripts/build-engine.sh), then the Cascades UI in the
# Native SDK 10.3.1 image, packaged unsigned with both binaries, the memory addon (../rlimit-patch, copied to
# build/headless-mem.zip) and the spawn helper hb-install-patcher (app/helper). Each package gets the next
# build id from build/buildid.txt, so the installer never keeps an older app.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
IMAGE=${BB10_NDK_IMAGE:-yamsergey/bb10-ndk:0.6}
"$ROOT/scripts/build-engine.sh" >/dev/null
"$ROOT/scripts/fetch-emoji.sh"
"$ROOT/scripts/fetch-qrcodegen.sh"
"$ROOT/scripts/fetch-omxil.sh"
"$ROOT/scripts/fetch-libwebp.sh"
# The app offers the headless-mem addon (app/src/MemoryPatch.cpp). The docker mount below covers only this
# project, so the zip has to be a real copy.
if [ ! -x "$ROOT/../rlimit-patch/build.sh" ]; then
	echo "build-app.sh: ../rlimit-patch is missing: the package bundles its headless-mem.zip" >&2
	exit 2
fi
"$ROOT/../rlimit-patch/build.sh" >/dev/null
mkdir -p "$ROOT/build"
cp "$ROOT/../rlimit-patch/build/headless-mem.zip" "$ROOT/build/headless-mem.zip"
[ -f "$ROOT/build/buildid.txt" ] || echo 1 >"$ROOT/build/buildid.txt"
docker run --rm -v "$ROOT:/src" -w /src "$IMAGE" bash -c '
	source /root/bbndk/bbndk-env_10_3_1_995.sh >/dev/null
	set -eu
	mkdir -p build/ui app/assets/translations
	qcc -Vgcc_ntoarmv7le -O2 -o build/ui/hb-install-patcher app/helper/hb-install-patcher.c
	# lupdate parses QML with Qt 4.8, as the phone does; the Qt 6 lint lets some syntax through
	# that fails a whole page there (Recorder.qml, 0.0.1.77).
	if lupdate app/assets/*.qml app/assets/*.js -ts /tmp/qml-check.ts 2>&1 | grep -A2 "error:"; then
		echo "build-app.sh: QML that Qt 4.8 cannot parse" >&2
		exit 1
	fi
	# lupdate takes `readonly property`, the phone refuses the page ("Readonly not yet supported": the
	# call screen never opened in 0.3.1.371-373).
	if grep -n "readonly property" app/assets/*.qml; then
		echo "build-app.sh: readonly properties fail on the phone" >&2
		exit 1
	fi
	for ts in app/translations/hyperberry_*.ts; do
		lrelease "$ts" -qm "app/assets/translations/$(basename "$ts" .ts).qm" >/dev/null
	done
	(cd build/ui && qmake -spec blackberry-armv7le-qcc CONFIG+=device ../../app/hyperberry.pro >/dev/null &&
		make -j"$(nproc)" >/dev/null)
	cd app && blackberry-nativepackager -package ../build/hyperberry.bar -buildIdFile ../build/buildid.txt \
		bar-descriptor.xml -configuration Device-Release
	rm -f assets/.assets.index
'
unzip -p "$ROOT/build/hyperberry.bar" META-INF/MANIFEST.MF | grep -E '^Package-(Id|Version):'
