package app

// SupportedOutputFormats is the authoritative list of output formats the
// MusterFlow CLI can render, in the order they are advertised to users. The
// generator renders it verbatim into the usage string of every generated
// operation's -o/--output flag, so it must name every format internal/cli can
// actually produce: table, json, yaml, csv, jsonl and parquet.
//
// The authority lives here, not in internal/cli: internal/cli imports
// internal/app, so the dependency only points one way. internal/cli's tests
// assert this list does not drift from the formats its output path handles.
var SupportedOutputFormats = []string{"table", "json", "yaml", "csv", "jsonl", "parquet"}

// DefaultOutputFormat is the format used when the caller passes no -o/--output
// flag and no output file extension implies one.
const DefaultOutputFormat = "table"
