package main

import (
	"net"
	"testing"
	"time"
)

// TestIsPortInUse_FreePort verifies that isPortInUse returns false when
// nothing is listening on the given address. This is the dashboard-NOT-running
// path — the code must still call registry.Load() in that case.
func TestIsPortInUse_FreePort(t *testing.T) {
	// Pick a random free port by opening and immediately closing a listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	if isPortInUse(addr) {
		t.Error("expected isPortInUse=false for a free port")
	}
}

// TestIsPortInUse_Listening verifies that isPortInUse returns true when
// something IS listening. This is the dashboard-running path where we skip
// LoadReadOnly and route through the dashboard API instead.
func TestIsPortInUse_Listening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	if !isPortInUse(ln.Addr().String()) {
		t.Error("expected isPortInUse=true for a listening port")
	}
}

// TestIsPortInUse_Timeout verifies the 200ms timeout does not hang
// when the address is unreachable.
func TestIsPortInUse_Timeout(t *testing.T) {
	// 192.0.2.1 is TEST-NET-1 (RFC 5737) — guaranteed to be unroutable.
	// The 200ms timeout ensures this returns quickly.
	start := time.Now()
	_ = isPortInUse("192.0.2.1:9999")
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Errorf("isPortInUse took %v, expected <2s (timeout should be 200ms)", elapsed)
	}
}