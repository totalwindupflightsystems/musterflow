package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// listeningPortForPID returns the TCP port pid is listening on that answers
// MusterFlow's dashboard health check, or 0 when it cannot be determined (no
// /proc, a different user, or the process is not serving HTTP).
//
// DF-045: the lock message has to name the port that actually serves the
// connected APIs. DuckDB only reports the PID, so the port is recovered from
// the lock holder's listening sockets and confirmed against /api/health rather
// than guessed.
func listeningPortForPID(pid int) int {
	if pid <= 0 {
		return 0
	}
	inodes := socketInodes(pid)
	if len(inodes) == 0 {
		return 0
	}

	var candidates []int
	seen := make(map[int]bool)
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		for _, port := range listenPorts(table, inodes) {
			if port == 0 || seen[port] {
				continue
			}
			seen[port] = true
			candidates = append(candidates, port)
		}
	}
	if len(candidates) > 8 {
		candidates = candidates[:8]
	}
	for _, port := range candidates {
		if isDashboardHealth(port) {
			return port
		}
	}
	return 0
}

// socketInodes returns the socket inode numbers that pid holds open.
func socketInodes(pid int) map[string]bool {
	dir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	inodes := make(map[string]bool)
	for _, entry := range entries {
		link, err := os.Readlink(filepath.Join(dir, entry.Name()))
		if err != nil || !strings.HasPrefix(link, "socket:[") {
			continue
		}
		inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] = true
	}
	return inodes
}

// listenPorts parses a /proc/net/tcp-style table and returns, in file order,
// the local ports of LISTEN sockets whose inode is in inodes.
func listenPorts(table string, inodes map[string]bool) []int {
	f, err := os.Open(table)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	var ports []int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// sl local_address rem_address st ... inode
		if len(fields) < 10 || fields[3] != "0A" { // 0A == TCP_LISTEN
			continue
		}
		socket := false
		for _, field := range fields[9:] {
			if inodes[field] {
				socket = true
				break
			}
		}
		if !socket {
			continue
		}
		colon := strings.LastIndex(fields[1], ":")
		if colon < 0 {
			continue
		}
		port, err := strconv.ParseUint(fields[1][colon+1:], 16, 16)
		if err != nil {
			continue
		}
		ports = append(ports, int(port))
	}
	return ports
}

// isDashboardHealth reports whether port serves MusterFlow's dashboard health
// endpoint. Other listeners on the same process (metrics, pprof, an unrelated
// tool) are rejected so the reported port is the one the CLI should use.
func isDashboardHealth(port int) bool {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var health struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return false
	}
	return health.Status == "ok"
}
