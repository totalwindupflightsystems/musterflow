// Command transform is a WASI module used by internal/wasm tests.
// It reads JSON from stdin and writes "transformed:" prepended to stdout.
// Built with: GOOS=wasip1 GOARCH=wasm go build -o ../transform.wasm .
package main

import (
	"io"
	"os"
)

func main() {
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(append([]byte("transformed:"), b...)); err != nil {
		os.Exit(1)
	}
}
