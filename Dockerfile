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

FROM golang:1.26-alpine AS builder

WORKDIR /app

# go-duckdb is a cgo wrapper around DuckDB's C library — the build needs gcc + musl-dev
RUN apk add --no-cache gcc musl-dev

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

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/musterflow /usr/local/bin/musterflow

EXPOSE 9876
VOLUME /root/.musterflow

ENTRYPOINT ["musterflow"]
CMD ["start"]
