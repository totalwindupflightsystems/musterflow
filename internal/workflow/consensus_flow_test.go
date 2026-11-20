package workflow

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// shippedConsensusFlow is the flow this test suite proves. It lives at the repo
// root so it ships with the module and can be copied into ~/.musterflow/flows.
const shippedConsensusFlow = "../../flows/consensus-e2e.star"

// consensusStub is a stand-in for a live Consensus instance: it implements the
// three routes the flow drives (create session, post message, get session) and
// records what it saw so a test can assert the flow really exercised the chain
// rather than merely returning 200s.
type consensusStub struct {
	t *testing.T

	// sessionStatus is the sequence of statuses returned by GET, one per poll.
	// The last entry repeats once the sequence is exhausted.
	sessionStatus []string
	// lastMessage is the value GET returns for last_message (nil => JSON null).
	lastMessage *string

	mu         sync.Mutex
	creates    int
	posts      int
	polls      int
	gotAuth    string
	gotContent string
	gotGoal    string
}

func (s *consensusStub) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.gotAuth = r.Header.Get("Authorization")
		s.mu.Unlock()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions":
			var body struct {
				AgentName string `json:"agent_name"`
				Goal      string `json:"goal"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			s.mu.Lock()
			s.creates++
			s.gotGoal = body.Goal
			s.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"sess-0123456789ab","status":"idle"}`)

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/message"):
			var body struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			s.mu.Lock()
			s.posts++
			s.gotContent = body.Content
			s.mu.Unlock()
			_, _ = io.WriteString(w, `{"status":"message_received"}`)

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/sessions/"):
			s.mu.Lock()
			idx := s.polls
			s.polls++
			s.mu.Unlock()
			status := "idle"
			if len(s.sessionStatus) > 0 {
				status = s.sessionStatus[len(s.sessionStatus)-1]
				if idx < len(s.sessionStatus) {
					status = s.sessionStatus[idx]
				}
			}
			payload := map[string]interface{}{"id": "sess-0123456789ab", "status": status}
			if s.lastMessage != nil {
				payload["last_message"] = *s.lastMessage
			} else if status == "running" {
				payload["last_message"] = nil
			}
			_ = json.NewEncoder(w).Encode(payload)

		default:
			http.NotFound(w, r)
		}
	})
}

// engineWithShippedFlow copies the shipped flow + sidecar into a temp flows dir
// and returns an engine bound to it.
func engineWithShippedFlow(t *testing.T) *Engine {
	t.Helper()
	dir := t.TempDir()
	// shippedConsensusFlow is the <base>.star path; the sidecar is <base>.star.json.
	base := strings.TrimSuffix(shippedConsensusFlow, ".star")
	for _, suffix := range []string{".star", ".star.json"} {
		data, err := os.ReadFile(base + suffix)
		if err != nil {
			t.Fatalf("read shipped flow %s: %v", base+suffix, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "consensus-e2e"+suffix), data, 0o644); err != nil {
			t.Fatalf("copy shipped flow: %v", err)
		}
	}
	return NewEngine(dir, "http://localhost:9876")
}

func strPtr(s string) *string { return &s }

// TestConsensusFlow_EndToEndGreen is the mandatory regression test for
// CONSENSUS-INT-002: the SHIPPED flow file runs the whole chain — create
// session, post message, poll, assert reply — against a Consensus-shaped
// server, and reports green only when the reply is present.
func TestConsensusFlow_EndToEndGreen(t *testing.T) {
	reply := "456789ab"
	stub := &consensusStub{
		t:             t,
		sessionStatus: []string{"running", "running", "idle"},
		lastMessage:   strPtr(reply),
	}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()

	e := engineWithShippedFlow(t)
	out, err := e.Run("consensus-e2e", map[string]interface{}{
		"base_url":        srv.URL,
		"api_key":         "cs_ak_test_key",
		"poll_interval":   0.01,
		"timeout_seconds": 10,
	})
	if err != nil {
		t.Fatalf("flow failed: %v\noutput:\n%s", err, out)
	}

	// The 4-step chain really ran.
	if stub.creates != 1 {
		t.Errorf("creates = %d, want 1", stub.creates)
	}
	if stub.posts != 1 {
		t.Errorf("posts = %d, want 1", stub.posts)
	}
	if stub.polls < 3 {
		t.Errorf("polls = %d, want >= 3 (flow must poll until the session settles)", stub.polls)
	}
	// The assertion token was derived from the created session and carried in
	// the posted message — a canned reply cannot satisfy it.
	if stub.gotContent == "" || !strings.Contains(stub.gotContent, "456789ab") {
		t.Errorf("posted content = %q, want it to carry the session-derived nonce 456789ab", stub.gotContent)
	}
	if stub.gotGoal == "" {
		t.Error("create session goal was empty; the flow must send a goal")
	}
	if stub.gotAuth != "Bearer cs_ak_test_key" {
		t.Errorf("Authorization = %q, want Bearer cs_ak_test_key", stub.gotAuth)
	}

	for _, want := range []string{
		"created session: sess-0123456789ab",
		"posted message: ",
		"assistant reply: " + reply,
		"CONSENSUS_E2E_OK session=sess-0123456789ab status=idle polls=3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// TestConsensusFlow_FailsWhenReplyAbsent is the falsification arm the acceptance
// criterion demands: the same shipped flow, the same green-path server, but the
// session settles with no assistant reply. The run MUST fail — a workflow that
// passes here would be tautological.
func TestConsensusFlow_FailsWhenReplyAbsent(t *testing.T) {
	stub := &consensusStub{
		t:             t,
		sessionStatus: []string{"idle"},
		lastMessage:   nil, // settled, but no reply
	}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()

	e := engineWithShippedFlow(t)
	out, err := e.Run("consensus-e2e", map[string]interface{}{
		"base_url":        srv.URL,
		"poll_interval":   0.01,
		"timeout_seconds": 10,
	})
	if err == nil {
		t.Fatalf("flow reported success with no assistant reply; output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "no assistant reply") {
		t.Errorf("error = %v, want it to name the missing assistant reply", err)
	}
	if strings.Contains(out, "CONSENSUS_E2E_OK") {
		t.Errorf("output printed the success marker on a failing run:\n%s", out)
	}
	if stub.posts != 1 {
		t.Errorf("posts = %d, want 1 (the poll must happen after a real post)", stub.posts)
	}
}

// TestConsensusFlow_PollOnlyArmAbsentReply proves the poll-only input (post=false,
// session_id given) fails when that pre-existing session has no reply — the
// shape used to falsify a live run without creating a new session.
func TestConsensusFlow_PollOnlyArmAbsentReply(t *testing.T) {
	stub := &consensusStub{t: t, sessionStatus: []string{"idle"}}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()

	e := engineWithShippedFlow(t)
	out, err := e.Run("consensus-e2e", map[string]interface{}{
		"base_url":        srv.URL,
		"session_id":      "sess-0123456789ab",
		"post":            false,
		"poll_interval":   0.01,
		"timeout_seconds": 10,
	})
	if err == nil {
		t.Fatalf("flow reported success for a session with no reply; output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "no assistant reply") {
		t.Errorf("error = %v, want it to name the missing assistant reply", err)
	}
	if stub.creates != 0 || stub.posts != 0 {
		t.Errorf("creates = %d, posts = %d, want 0/0 for the poll-only arm", stub.creates, stub.posts)
	}
	if stub.polls < 1 {
		t.Errorf("polls = %d, want >= 1", stub.polls)
	}
}

// TestConsensusFlow_FailsOnWrongToken proves a present-but-unrelated reply also
// fails: the assertion is on the reply's content, not merely its existence.
func TestConsensusFlow_FailsOnWrongToken(t *testing.T) {
	stub := &consensusStub{
		t:             t,
		sessionStatus: []string{"idle"},
		lastMessage:   strPtr("I have summarized the ledger for you."),
	}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()

	e := engineWithShippedFlow(t)
	out, err := e.Run("consensus-e2e", map[string]interface{}{
		"base_url":        srv.URL,
		"poll_interval":   0.01,
		"timeout_seconds": 10,
	})
	if err == nil {
		t.Fatalf("flow accepted a reply that does not carry the expected token; output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "expected token") {
		t.Errorf("error = %v, want it to name the expected token mismatch", err)
	}
}

// TestConsensusFlow_FailsWhenSessionFails proves a session that settles in a
// failure state is reported as a failure, not silently polled out.
func TestConsensusFlow_FailsWhenSessionFails(t *testing.T) {
	stub := &consensusStub{t: t, sessionStatus: []string{"failed"}}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()

	e := engineWithShippedFlow(t)
	out, err := e.Run("consensus-e2e", map[string]interface{}{
		"base_url":        srv.URL,
		"poll_interval":   0.01,
		"timeout_seconds": 10,
	})
	if err == nil {
		t.Fatalf("flow reported success for a failed session; output:\n%s", out)
	}
	if !strings.Contains(err.Error(), `reached "failed"`) {
		t.Errorf("error = %v, want it to report the failed session status", err)
	}
}

// TestConsensusFlow_FailsWhenNeverSettles proves the poll budget is enforced:
// a session stuck in "running" past timeout_seconds raises instead of hanging.
func TestConsensusFlow_FailsWhenNeverSettles(t *testing.T) {
	stub := &consensusStub{t: t, sessionStatus: []string{"running"}}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()

	e := engineWithShippedFlow(t)
	out, err := e.Run("consensus-e2e", map[string]interface{}{
		"base_url":        srv.URL,
		"poll_interval":   0.02,
		"timeout_seconds": 1,
	})
	if err == nil {
		t.Fatalf("flow reported success for a session that never settled; output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "did not settle within") {
		t.Errorf("error = %v, want it to name the poll timeout", err)
	}
	if stub.polls < 2 {
		t.Errorf("polls = %d, want >= 2 (the flow must poll more than once before giving up)", stub.polls)
	}
}

// TestConsensusFlow_CreateFailureSurfaces proves a non-201 from the create step
// is an error that carries the server's message, so an auth problem is
// diagnosable from the flow output instead of looking like an empty reply.
func TestConsensusFlow_CreateFailureSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid api key"}`)
	}))
	defer srv.Close()

	e := engineWithShippedFlow(t)
	_, err := e.Run("consensus-e2e", map[string]interface{}{
		"base_url":        srv.URL,
		"poll_interval":   0.01,
		"timeout_seconds": 5,
	})
	if err == nil {
		t.Fatal("expected an error when the create call is unauthorized")
	}
	if !strings.Contains(err.Error(), "expected HTTP 201") || !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error = %v, want the expected-status note and the server message", err)
	}
}
