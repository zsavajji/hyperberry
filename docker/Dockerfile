# Build image for HyperBerry: bb10-cxx20 (clang 19 for QNX 6.6/ARMv7, the C compiler cgo needs)
# plus XIAZY's go-qnx (Go 1.27.2 with GOOS=qnx GOARCH=arm). go-qnx's README says to pin its
# image by digest: its go1.27.2 has Google's version string but not Google's bytes.
FROM ghcr.io/xiazy/go-qnx:1.27.2@sha256:1915ce0568acda8de04c82be016a47fa267858aef4b7c7b77402363770c04669 AS go
FROM bb10-cxx20:latest
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH GOTOOLCHAIN=local
