# syntax=docker/dockerfile:1
# MusterFlow — multi-stage Docker build
#
# The muster engine (github.com/wojons/muster) is a PUBLISHED Go module pinned
# in go.mod/go.sum, so the image builds from this repository's context alone —
# no engine checkout, no extra build context, no registry image:
#   docker build -t musterflow .
#   docker compose up -d --build        # the documented deploy path
#
# Engine DEVELOPMENT against a local checkout is a HOST-side concern and is NOT
# baked into the image: point MUSTER_ENGINE_DIR at the checkout (or clone it as
# ../muster) and run scripts/resolve-engine.sh to generate go.work, which is
# gitignored. Inside the image the resolver only runs to clear a stale go.work
# that `COPY . .` may have carried in from the host; it never edits go.mod.

FROM golang:1.26-bookworm AS builder

WORKDIR /app

# go-duckdb's prebuilt libduckdb.a is glibc-flavored (fortify symbols
# __vsnprintf_chk/__memcpy_chk) — it CANNOT link on musl/alpine; use the
# Debian-based image + gcc/g++ (libstdc++ needed at link time)
RUN apt-get update && apt-get install -y --no-install-recommends gcc g++ && rm -rf /var/lib/apt/lists/*

# Copy go.mod/go.sum first so `go mod download` is cached independently of the
# source tree. scripts/resolve-engine.sh is THE single engine-resolution
# mechanism: with no engine checkout in the image it is informational only (it
# may clear a stale go.work) and the module graph resolves the PUBLISHED
# muster module. No sed surgery or module-edit commands anywhere.
COPY go.mod go.sum ./
COPY scripts/resolve-engine.sh ./scripts/
RUN bash scripts/resolve-engine.sh && \
    go mod download

COPY . .

# Re-generate go.work: `COPY . .` may restore a host-side go.work that points
# at a path which does not exist in-container.
RUN bash scripts/resolve-engine.sh && \
    CGO_ENABLED=1 go build -ldflags="-s -w" -o musterflow ./cmd/musterflow/

# Runtime must be glibc-based too: the CGO binary links glibc dynamically
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/musterflow /usr/local/bin/musterflow

EXPOSE 9876
VOLUME /root/.musterflow

ENTRYPOINT ["musterflow"]
CMD ["start"]
