# syntax=docker/dockerfile:1
# MusterFlow — multi-stage Docker build
#
# Local build (requires muster engine at ../muster):
#   docker build -t musterflow --build-context muster=../muster .
#
# CI build:
#   docker build -t musterflow --build-context muster=./muster .
#
# The muster engine is a local dependency — provide it via build context.
# Without it, the replace directive in go.mod will fail.

FROM golang:1.26-bookworm AS builder

WORKDIR /app

# go-duckdb's prebuilt libduckdb.a is glibc-flavored (fortify symbols
# __vsnprintf_chk/__memcpy_chk) — it CANNOT link on musl/alpine; use the
# Debian-based image + gcc/g++ (libstdc++ needed at link time)
RUN apt-get update && apt-get install -y --no-install-recommends gcc g++ && rm -rf /var/lib/apt/lists/*

# Copy muster engine from build context
COPY --from=muster . ./muster

COPY go.mod go.sum ./
# Point the replace directive at the in-container engine path BEFORE
# downloading, so `go mod download` can resolve the module graph
# (works for both CI: go.mod sed'd to ./muster, and local builds: /home/kara/muster)
RUN go mod edit -replace github.com/wojons/muster=./muster && \
    go mod download

COPY . .

# Re-assert the in-container replace: `COPY . .` restores the checkout's go.mod
RUN go mod edit -replace github.com/wojons/muster=./muster && \
    go mod tidy

RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o musterflow ./cmd/musterflow/

# Runtime must be glibc-based too: the CGO binary links glibc dynamically
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/musterflow /usr/local/bin/musterflow

EXPOSE 9876
VOLUME /root/.musterflow

ENTRYPOINT ["musterflow"]
CMD ["start"]
