# ─────────────────────────────────────────────────────────────────────────
# consensus-e2e.star — drive a live Consensus instance end to end.
#
#   create session -> post message -> poll until settled -> assert that the
#   assistant reply is actually present
#
# Install either way:
#   cp flows/consensus-e2e.star flows/consensus-e2e.star.json ~/.musterflow/flows/
#   # or
#   musterflow flow create --name consensus-e2e \
#       --source "$(cat flows/consensus-e2e.star)" \
#       --description "Drive a live Consensus instance end to end"
#
# Run from the CLI:
#   musterflow flow run consensus-e2e \
#       --payload '{"base_url":"http://127.0.0.1:8090","api_key":"cs_ak_..."}'
#
# Run from the dashboard (same flow, same payload under the trigger key):
#   curl -s -X POST http://localhost:9876/api/flows/consensus-e2e/run \
#       -H 'Content-Type: application/json' \
#       -d '{"trigger":{"base_url":"http://127.0.0.1:8090","api_key":"cs_ak_..."}}'
#
# Trigger keys (all optional):
#   base_url        Consensus base URL                       (default http://127.0.0.1:8090)
#   api_key         Bearer token                             (default: header omitted)
#   agent_name      agent name stored on the session         (default musterflow-e2e)
#   goal            session goal                             (default: the text below)
#   prompt          user message posted to the session       (default: echo the nonce)
#   expect          substring the assistant reply must hold  (default: the nonce;
#                                                             "" disables the check)
#   session_id      poll this existing session, skip creation
#   post            False => poll without posting a message  (absent-reply arm)
#   timeout_seconds overall poll budget                      (default 120)
#   poll_interval   seconds between polls                    (default 2)
#   verbose         True => print every poll                 (default False)
#
# THE ASSERTION IS THE DELIVERABLE. The flow raises — non-zero exit from the
# CLI, HTTP 500 with the message from the dashboard — when the session never
# settles, settles in a failed/paused state, or settles with an empty
# last_message. It does not print "ok" merely because the HTTP calls returned
# 200, so a run that proves nothing cannot report green.
#
# The nonce is derived from the session id the server just minted, so the
# substring check cannot be satisfied by a canned reply that predates the run.
# ─────────────────────────────────────────────────────────────────────────

SETTLED_STATUSES = ["idle", "completed"]
FAILED_STATUSES = ["failed", "paused", "cancelled", "canceled"]
DEFAULT_GOAL = "MusterFlow consensus end-to-end check: reply with the token you are given."


# _cfg reads one trigger key, falling back to a default when the payload is
# absent (CLI run with no --payload) or the key was not supplied.
def _cfg(key, default):
    if trigger == None:
        return default
    value = trigger.get(key)
    if value == None:
        return default
    return value


def _headers(api_key):
    headers = {"Content-Type": "application/json"}
    if api_key != "":
        headers["Authorization"] = "Bearer " + api_key
    return headers


# _nonce derives the assertion token from the session id: the hex tail with
# separators stripped, so it is unique per run and stable to quote in a prompt.
def _nonce(session_id):
    compact = session_id.replace("-", "")
    if len(compact) > 8:
        return compact[len(compact) - 8:]
    return compact


def _require_status(label, resp, want):
    if resp["status"] != want:
        fail("%s: expected HTTP %d, got %d: %s" % (label, want, resp["status"], resp["body"]))


def main():
    base = _cfg("base_url", "http://127.0.0.1:8090")
    api_key = _cfg("api_key", "")
    headers = _headers(api_key)
    timeout = int(_cfg("timeout_seconds", 120))
    interval = float(_cfg("poll_interval", 2))
    verbose = _cfg("verbose", False)
    post = _cfg("post", True)
    session_id = _cfg("session_id", "")

    if interval <= 0:
        fail("poll_interval must be greater than 0, got " + str(interval))
    if timeout <= 0:
        fail("timeout_seconds must be greater than 0, got " + str(timeout))

    # ── 1. create session ────────────────────────────────────────────────
    if session_id == "":
        created = http_post(
            base + "/api/v1/sessions",
            body=json.encode({
                "agent_name": _cfg("agent_name", "musterflow-e2e"),
                "goal": _cfg("goal", DEFAULT_GOAL),
            }),
            headers=headers,
        )
        _require_status("create session", created, 201)
        session = json.decode(created["body"])
        session_id = str(session["id"])
        print("created session: " + session_id)
    else:
        session_id = str(session_id)
        print("polling existing session: " + session_id)

    nonce = _nonce(session_id)
    expect = _cfg("expect", nonce)

    # ── 2. post the user message ─────────────────────────────────────────
    if post:
        prompt = str(_cfg("prompt", "Reply with exactly this token and nothing else: " + nonce))
        sent = http_post(
            base + "/api/v1/sessions/" + session_id + "/message",
            body=json.encode({"role": "user", "content": prompt}),
            headers=headers,
        )
        _require_status("post message", sent, 200)
        print("posted message: " + prompt)
    else:
        print("skipped posting (post=false)")

    # ── 3. poll until the session settles ────────────────────────────────
    attempts = int(timeout / interval) + 1
    settled = False
    status = "unknown"
    reply = None
    polls = 0
    for n in range(attempts):
        polled = http_get(base + "/api/v1/sessions/" + session_id, headers=headers)
        _require_status("poll session", polled, 200)
        state = json.decode(polled["body"])
        status = state.get("status") or "unknown"
        reply = state.get("last_message")
        polls = n + 1
        if verbose:
            had_reply = "no" if reply == None or reply == "" else "yes"
            print("poll %d: status=%s reply=%s" % (polls, status, had_reply))
        if status in SETTLED_STATUSES:
            settled = True
            break
        if status in FAILED_STATUSES:
            fail("session %s reached %r instead of answering (%d polls): %s" % (
                session_id, status, polls, polled["body"]))
        sleep(interval)

    # ── 4. assert the assistant reply is present ──────────────────────────
    if not settled:
        fail("session %s did not settle within %ds: last status %r after %d polls" % (
            session_id, timeout, status, polls))
    if reply == None or str(reply) == "":
        fail("no assistant reply: session %s settled as %r with an empty last_message after %d polls" % (
            session_id, status, polls))
    reply = str(reply)
    if expect != "" and str(expect) not in reply:
        fail("assistant reply does not contain the expected token %r: %r" % (str(expect), reply))

    print("assistant reply: " + reply)
    print("CONSENSUS_E2E_OK session=%s status=%s polls=%d" % (session_id, status, polls))


main()
