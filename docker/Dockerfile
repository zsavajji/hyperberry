# Build image for HyperBerry: bb10-cxx20 (clang 19 for QNX 6.6/ARMv7, the C compiler cgo needs)
# plus go-qnx (Go 1.27.4 with GOOS=qnx GOARCH=arm), from the fork zsavajji/go-qnx. Pinned by
# digest, as go-qnx's README says: its go1.27.4 has Google's version string but not Google's bytes.
FROM ghcr.io/zsavajji/go-qnx:1.27.4@sha256:19f06aa6606912a3fd89c1e4eca226292a9fb1071ec57d2d09b5b3ee8529b094 AS go
# 19.1.7-6: NEON by default, temp_directory_path() falls back to /var/tmp (the user's ask, 2026-11-27).
FROM bb10-cxx20:19.1.7-6-build
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH GOTOOLCHAIN=local

# libopus for voice notes (PLAN.md §7, step 0.8): one pinned release, built static twice, for
# BB10 into the SDK sysroot (bb10-clang finds it there) and for the PC tests into /usr/local.
# Both install the headers as <opus/opus.h>.
ADD --checksum=sha256:6ffcb593207be92584df15b32466ed64bbec99109f007c82205f0194572411a1 \
	https://downloads.xiph.org/releases/opus/opus-1.6.1.tar.gz /tmp/opus.tar.gz
# QNX has no runtime CPU detection, so the ARM code paths are fixed at configure time: NEON is
# presumed, as every BB10 CPU has it (Krait, Cortex-A9). Code for features the flags don't
# presume (ARMv8 dotprod) is built but never called.
RUN cd /tmp && tar xzf opus.tar.gz && cd opus-1.6.1 \
	&& flags="--enable-static --disable-shared --disable-doc --disable-extra-programs" \
	&& T=/opt/bb10-sdk/target/qnx6 \
	&& ./configure --host=arm-unknown-nto-qnx6.5.0eabi CC=bb10-clang CFLAGS="-O2 -mfpu=neon" \
		--disable-rtcd $flags \
		--prefix=$T/armle-v7/usr --includedir=$T/usr/include >/dev/null \
	&& make -j"$(nproc)" >/dev/null && make install >/dev/null && make distclean >/dev/null \
	&& ./configure CC=cc $flags --prefix=/usr/local >/dev/null \
	&& make -j"$(nproc)" >/dev/null && make install >/dev/null \
	&& cd / && rm -rf /tmp/opus*

# For engine/framesocket.patch (scripts/hypermeow-work.sh).
RUN apt-get update >/dev/null && apt-get install -y --no-install-recommends patch >/dev/null
