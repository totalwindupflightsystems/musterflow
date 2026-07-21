// Package cli provides the OAuth2 callback server for the CLI.
package cli

import (
	"fmt"
	"net/http"
	"time"
)

// startCallbackServer starts an HTTP server on the given port and waits for
// a single OAuth2 callback. Returns the authorization code from the query.
func startCallbackServer(port int) (string, error) {
	type result struct {
		code string
		err  error
	}
	ch := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code := q.Get("code")
		errStr := q.Get("error")
		if errStr != "" {
			ch <- result{err: fmt.Errorf("authorization error: %s (%s)", errStr, q.Get("error_description"))}
			http.Error(w, "Authorization failed", http.StatusBadRequest)
			return
		}
		if code == "" {
			ch <- result{err: fmt.Errorf("no authorization code received")}
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			return
		}
		ch <- result{code: code}
		fmt.Fprint(w, "Authorization successful! You may close this window.")
	})

	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		// Server will exit on Shutdown or error
		_ = srv.ListenAndServe()
	}()

	// Wait for callback or timeout
	select {
	case res := <-ch:
		srv.Close()
		if res.err != nil {
			return "", res.err
		}
		return res.code, nil
	case <-time.After(5 * time.Minute):
		srv.Close()
		return "", fmt.Errorf("timed out waiting for authorization (5 minutes)")
	}
}
