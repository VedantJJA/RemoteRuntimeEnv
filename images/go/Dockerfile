# Build cache disabled via GOCACHE=/tmp so it lands on the writable tmpfs
# mount, not the read-only rootfs.
FROM golang:1.22-alpine
RUN adduser -D -u 1000 sandbox
ENV GOCACHE=/tmp/gocache
USER sandbox
WORKDIR /sandbox
