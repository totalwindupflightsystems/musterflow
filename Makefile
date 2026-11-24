# Canonical build targets for musterflow.
#
# `make build` stamps main.Version with the current HEAD via git describe;
# a plain `go build ./...` (or `go install`) keeps the default 0.1.0 from
# cmd/musterflow/main.go.

.PHONY: build test vet fmt

build:
	go build -o bin/musterflow -ldflags "-X main.Version=$$(git describe --always --dirty)" ./cmd/musterflow

test:
	go test ./...

vet:
	go vet ./...

fmt:
	go fmt ./...
