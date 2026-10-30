# Base images are pinned by multi-arch index digest to guard against tag
# tampering. The tag is informational; update tag and digest together.
ARG GO_IMAGE=golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb
ARG ALPINE_IMAGE=alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

# Build a fully static binary. The build stage runs on the native platform
# and cross-compiles for the target, so no emulation is needed.
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
ARG BUILD_DATE=unknown

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath \
      -ldflags "-s -w -X 'main.VERSION=${VERSION}' -X 'main.BUILD_DATE=${BUILD_DATE}'" \
      -o /out/pgbouncer-healthcheck . && \
    go vet ./...

# Integration test image: real PGBouncer plus the test scripts
FROM ${ALPINE_IMAGE} AS test

# Newer pgbouncer packages no longer create a pgbouncer user
RUN apk add --no-cache pgbouncer bash curl jq sudo && \
    adduser -S -D -H pgbouncer && \
    mkdir -p /var/run/postgresql && \
    chown pgbouncer /var/run/postgresql

COPY tests/pgbouncer.ini /etc/pgbouncer/pgbouncer.ini
COPY tests/userlist.txt /etc/pgbouncer/userlist.txt
COPY --chmod=755 tests/scripts /tests
COPY --from=build /out/pgbouncer-healthcheck /usr/local/bin/pgbouncer-healthcheck

WORKDIR /tests

# Minimal runtime image containing only the static binary
FROM scratch AS runtime

COPY --from=build /out/pgbouncer-healthcheck /pgbouncer-healthcheck

EXPOSE 8000
ENTRYPOINT ["/pgbouncer-healthcheck"]
