#!/bin/sh
# Makes app/assets/video/test640.264, the clip of Settings -> Video decoder test: ffmpeg's test
# pattern, 640x480 at 30 fps for 10 s, H.264 baseline level 3.1, no B-frames, an IDR every 2 s,
# raw Annex-B. The result is committed (about 130 KB); run this only to change it.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
ffmpeg -y -loglevel error -f lavfi -i testsrc=size=640x480:rate=30:duration=10 \
	-c:v libx264 -profile:v baseline -level 3.1 -bf 0 -g 60 -pix_fmt yuv420p -f h264 \
	"$ROOT/app/assets/video/test640.264"
