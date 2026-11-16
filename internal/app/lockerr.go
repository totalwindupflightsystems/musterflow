package app

import (
	"regexp"
	"strconv"
	"strings"
)

// StoreLock describes a DuckDB single-writer lock conflict: the store could not
// be opened because another process already holds the lock on the database
// file. Customarily that process is a running MusterFlow dashboard — so the CLI
// is able to reach the connected APIs only by routing through that dashboard's
// port.
//
// The details are parsed from DuckDB's error text: the driver (an engine
// dependency) returns a free-form message, not a typed "locked" error, and
// musterflow classifies it here rather than patching the engine. See DF-045.
type StoreLock struct {
	// DBPath is the database file named by DuckDB ("" when not named).
	DBPath string
	// Holder is the executable path of the lock-holding process ("" when not reported).
	Holder string
	// PID is the lock-holding process id (0 when not reported).
	PID int
	// Port is the port the lock holder listens on, verified to answer
	// MusterFlow's /api/health, or 0 when it could not be determined.
	Port int
	// Err is the original store-open error.
	Err error
}

// lockedFilePattern matches the file DuckDB reports it could not lock.
var lockedFilePattern = regexp.MustCompile(`Could not set lock on file "([^"]+)"`)

// lockHolderPattern matches DuckDB's "Conflicting lock is held in <exe> (PID n)".
// The executable path may itself contain spaces or parentheses, so the match is
// non-greedy up to the "(PID " marker.
var lockHolderPattern = regexp.MustCompile(`Conflicting lock is held in (.*?) \(PID (\d+)\)`)

// AsStoreLock reports whether err is a DuckDB lock conflict and, if so, the
// lock holder it describes. It returns false for every other store-open failure
// (missing directory, permissions, corruption), which callers report verbatim.
func AsStoreLock(err error) (*StoreLock, bool) {
	if err == nil {
		return nil, false
	}
	msg := err.Error()
	if !strings.Contains(msg, "Could not set lock on file") &&
		!strings.Contains(msg, "Conflicting lock is held") {
		return nil, false
	}

	lock := &StoreLock{Err: err}
	if m := lockedFilePattern.FindStringSubmatch(msg); m != nil {
		lock.DBPath = m[1]
	}
	if m := lockHolderPattern.FindStringSubmatch(msg); m != nil {
		lock.Holder = strings.TrimSpace(m[1])
		lock.PID, _ = strconv.Atoi(m[2])
	}
	if lock.PID > 0 {
		lock.Port = listeningPortForPID(lock.PID)
	}
	return lock, true
}
