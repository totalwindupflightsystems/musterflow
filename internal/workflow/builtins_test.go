package workflow

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeFlow writes source as <name>.star into dir and returns an Engine bound to
// dir.
func writeFlow(t *testing.T, dir, name, source string) *Engine {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".star"), []byte(source), 0o644); err != nil {
		t.Fatalf("write flow: %v", err)
	}
	return NewEngine(dir, "http://localhost:9876")
}

// ── json module ─────────────────────────────────────────────────────────

// TestRun_JSONDecode proves a flow can decode a response body into structured
// Starlark values instead of substring-matching raw JSON. This is what lets an
// integration flow assert on a FIELD (e.g. a session's last_message) rather
// than only on the HTTP status.
func TestRun_JSONDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"sess-1","status":"idle","last_message":"hello there","nested":{"tokens":42}}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	src := `
def main():
    resp = http_get("` + srv.URL + `/api/v1/sessions/sess-1")
    body = json.decode(resp["body"])
    print("id=" + body["id"])
    print("status=" + body["status"])
    print("reply=" + str(body["last_message"]))
    print("tokens=" + str(body["nested"]["tokens"]))

main()
`
	e := writeFlow(t, dir, "decode", src)
	out, err := e.Run("decode", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"id=sess-1", "status=idle", "reply=hello there", "tokens=42"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// TestRun_JSONDecodeNullIsNone proves a JSON null decodes to None, which is how
// the consensus flow distinguishes "no assistant reply yet" from a reply.
func TestRun_JSONDecodeNullIsNone(t *testing.T) {
	dir := t.TempDir()
	src := `
def main():
    body = json.decode('{"last_message": null}')
    if body["last_message"] == None:
        print("absent")
    else:
        print("present")

main()
`
	e := writeFlow(t, dir, "nullfield", src)
	out, err := e.Run("nullfield", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(out) != "absent" {
		t.Errorf("output = %q, want absent", out)
	}
}

// TestRun_JSONEncode proves a flow can build a request body with json.encode
// and that the encoded object reaches the server intact.
func TestRun_JSONEncode(t *testing.T) {
	var got map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("server could not decode body %q: %v", raw, err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"sess-2"}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	src := `
def main():
    resp = http_post("` + srv.URL + `/api/v1/sessions",
                     body=json.encode({"agent_name": "demo", "goal": "summarize"}),
                     headers={"Content-Type": "application/json"})
    print("status=" + str(resp["status"]))

main()
`
	e := writeFlow(t, dir, "encode", src)
	out, err := e.Run("encode", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "status=201") {
		t.Errorf("output = %q, want status=201", out)
	}
	if got["agent_name"] != "demo" || got["goal"] != "summarize" {
		t.Errorf("server received %v, want agent_name=demo goal=summarize", got)
	}
}

// ── sleep ───────────────────────────────────────────────────────────────

// TestRun_SleepWaits proves sleep(seconds) actually blocks, so a poll loop
// cannot spin. The assertion is a lower bound on elapsed wall time.
func TestRun_SleepWaits(t *testing.T) {
	dir := t.TempDir()
	e := writeFlow(t, dir, "sleeper", "def main():\n    sleep(0.15)\n    print('done')\n\nmain()\n")

	start := time.Now()
	out, err := e.Run("sleeper", nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(out) != "done" {
		t.Errorf("output = %q, want done", out)
	}
	if elapsed < 150*time.Millisecond {
		t.Errorf("elapsed = %s, want >= 150ms (sleep did not block)", elapsed)
	}
}

// TestRun_SleepArgumentErrors proves the sleep argument is validated: a
// negative or oversized delay is an error rather than a silent no-op or an
// unbounded pause that would pin the dashboard handler.
func TestRun_SleepArgumentErrors(t *testing.T) {
	cases := []struct {
		name string
		call string
		want string
	}{
		{"negative", "sleep(-1)", "must not be negative"},
		{"over cap", "sleep(31)", "must be at most"},
		{"wrong type", `sleep("soon")`, "must be a number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			e := writeFlow(t, dir, "badsleep", "def main():\n    "+tc.call+"\n\nmain()\n")
			_, err := e.Run("badsleep", nil)
			if err == nil {
				t.Fatalf("%s: expected an error, got nil", tc.call)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
