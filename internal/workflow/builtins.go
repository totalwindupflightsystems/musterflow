package workflow

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.starlark.net/starlark"
)

// HTTP builtins available to every flow.
//
// The engine predeclares three globals so a flow can chain API calls without
// shelling out:
//
//	http_get(url, headers=None)                       -> response dict
//	http_post(url, body=None, headers=None)           -> response dict
//	http_request(method, url, headers=None, body=None) -> response dict
//
// Every builtin returns the same response dict with exactly three keys:
// "status" (int), "body" (string) and "headers" (dict of str -> str holding the
// first value of each canonical header name). HTTP status codes are data, not
// errors: a 404 or 500 returns a normal dict so flows can branch on
// resp["status"]. Only transport-level failures (DNS, dial, TLS, timeout) raise
// a Starlark error, prefixed with the failing builtin name.
const (
	// flowHTTPTimeout bounds a single builtin HTTP request.
	flowHTTPTimeout = 10 * time.Second
	// flowMaxResponseBody caps how many response bytes are exposed to a flow.
	// Larger bodies are truncated (the flow still gets a usable dict) so a
	// misbehaving upstream cannot make the daemon read an unbounded body.
	flowMaxResponseBody = 1 << 20 // 1 MiB
)

// flowHTTPMethods is the closed set of methods http_request accepts. The method
// string is upper-cased before validation; anything outside this set is an
// error naming the valid methods.
var flowHTTPMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// flowHTTPClient performs builtin requests. A shared client keeps a connection
// pool across calls; the per-request deadline comes from Client.Timeout.
var flowHTTPClient = &http.Client{Timeout: flowHTTPTimeout}

// flowBuiltins returns the predeclared globals supplied to every flow. It is
// the single source of truth for both the compiler's isPredeclared callback and
// the StringDict passed to Program.Init, so a flow can never compile against a
// builtin it cannot resolve at run time.
func flowBuiltins() starlark.StringDict {
	return starlark.StringDict{
		"http_get":     starlark.NewBuiltin("http_get", httpGetBuiltin),
		"http_post":    starlark.NewBuiltin("http_post", httpPostBuiltin),
		"http_request": starlark.NewBuiltin("http_request", httpRequestBuiltin),
	}
}

// httpGetBuiltin implements http_get(url, headers=None).
func httpGetBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		url     string
		headers starlark.Value = starlark.None
	)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "url", &url, "headers?", &headers); err != nil {
		return nil, err
	}
	return doHTTP(b.Name(), http.MethodGet, url, headers, starlark.None)
}

// httpPostBuiltin implements http_post(url, body=None, headers=None).
func httpPostBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		url     string
		body    starlark.Value = starlark.None
		headers starlark.Value = starlark.None
	)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"url", &url, "body?", &body, "headers?", &headers); err != nil {
		return nil, err
	}
	return doHTTP(b.Name(), http.MethodPost, url, headers, body)
}

// httpRequestBuiltin implements http_request(method, url, headers=None, body=None).
func httpRequestBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		method  string
		url     string
		headers starlark.Value = starlark.None
		body    starlark.Value = starlark.None
	)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"method", &method, "url", &url, "headers?", &headers, "body?", &body); err != nil {
		return nil, err
	}

	normalized := strings.ToUpper(strings.TrimSpace(method))
	for _, m := range flowHTTPMethods {
		if normalized == m {
			return doHTTP(b.Name(), m, url, headers, body)
		}
	}
	return nil, fmt.Errorf("%s: invalid method %q: valid methods are %s",
		b.Name(), method, strings.Join(flowHTTPMethods, ", "))
}

// doHTTP runs one request and converts the reply into the flow response dict.
// name is the calling builtin, used to prefix every error so a flow author can
// tell which call failed.
func doHTTP(name, method, url string, headersVal, bodyVal starlark.Value) (starlark.Value, error) {
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("%s: url must be a non-empty string", name)
	}

	body, err := bodyBytes(name, bodyVal)
	if err != nil {
		return nil, err
	}
	if len(body) > 0 && (method == http.MethodGet || method == http.MethodHead) {
		return nil, fmt.Errorf("%s: %s requests must not include a body", name, method)
	}

	hdrs, err := headerPairs(name, headersVal)
	if err != nil {
		return nil, err
	}

	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}

	resp, err := flowHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read one byte past the cap so truncation is detected without reading an
	// unbounded body.
	data, err := io.ReadAll(io.LimitReader(resp.Body, flowMaxResponseBody+1))
	if err != nil {
		return nil, fmt.Errorf("%s: read response body: %w", name, err)
	}
	if len(data) > flowMaxResponseBody {
		data = data[:flowMaxResponseBody]
	}

	respHeaders := starlark.NewDict(len(resp.Header))
	for k, v := range resp.Header {
		if len(v) == 0 {
			continue
		}
		_ = respHeaders.SetKey(starlark.String(k), starlark.String(v[0]))
	}

	result := starlark.NewDict(3)
	_ = result.SetKey(starlark.String("status"), starlark.MakeInt(resp.StatusCode))
	_ = result.SetKey(starlark.String("body"), starlark.String(string(data)))
	_ = result.SetKey(starlark.String("headers"), respHeaders)
	return result, nil
}

// bodyBytes validates the optional body argument. Accepts a string or bytes;
// None and the empty string mean "no body".
func bodyBytes(name string, v starlark.Value) ([]byte, error) {
	switch val := v.(type) {
	case nil, starlark.NoneType:
		return nil, nil
	case starlark.String:
		return []byte(string(val)), nil
	case starlark.Bytes:
		return []byte(string(val)), nil
	default:
		return nil, fmt.Errorf("%s: body must be a string or bytes, got %s", name, v.Type())
	}
}

// headerPairs validates the optional headers argument: a dict of str -> str.
// None means "no headers".
func headerPairs(name string, v starlark.Value) (map[string]string, error) {
	if v == nil || v == starlark.None {
		return nil, nil
	}
	dict, ok := v.(*starlark.Dict)
	if !ok {
		return nil, fmt.Errorf("%s: headers must be a dict of str to str, got %s", name, v.Type())
	}
	out := make(map[string]string, dict.Len())
	for _, item := range dict.Items() {
		key, ok := item[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("%s: headers keys must be strings, got %s", name, item[0].Type())
		}
		val, ok := item[1].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("%s: headers values must be strings, got %s for key %q",
				name, item[1].Type(), string(key))
		}
		out[string(key)] = string(val)
	}
	return out, nil
}
