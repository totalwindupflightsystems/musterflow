// Package cli provides output format writers.
package cli

import (
	"fmt"
	"io"
)

// writeParquet writes data as a Parquet file. This is a stub — the full
// implementation requires github.com/parquet-go/parquet-go.
// To enable Parquet support, run:
//
//	go get github.com/parquet-go/parquet-go@latest
//
// and replace this stub with the real implementation in parquet_full.go.
func writeParquet(w io.Writer, data interface{}) error {
	return fmt.Errorf("Parquet support is not yet compiled in. " +
		"To enable: go get github.com/parquet-go/parquet-go@latest && " +
		"go build -tags parquet ./...")
}
