# syntax=docker/dockerfile:1
# MusterFlow — multi-stage Docker build
#
# Local build (requires muster engine at ../muster):
#   docker build -t musterflow --build-context muster=../muster .
#
# CI build:
#   docker build -t musterflow --build-context muster=/tmp/muster .
#
# The muster engine is a local dependency — provide it via build context.
# Without it, the replace directive in go.mod will fail.

FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy muster engine from build context
COPY --from=muster . /tmp/muster

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Replace local muster path with build-context path
RUN go mod edit -replace github.com/wojons/muster=/tmp/muster && \
    go mod tidy

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o musterflow ./cmd/musterflow/

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/musterflow /usr/local/bin/musterflow

EXPOSE 9876
VOLUME /root/.musterflow

ENTRYPOINT ["musterflow"]
CMD ["start"]
