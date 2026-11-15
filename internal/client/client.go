// Package client is the remote-mode Backend of the issues CLI: a thin
// HTTP client over /api/v1 (docs/sketch/ja/cli-and-web-api.md, Phase 3).
// It speaks the api package's DTOs — the single source for the wire
// format — and converts them to the store package's domain types so the
// CLI sees the same types either mode.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Client calls the JSON API on a remote issues server. The zero-ish use is
// New(server, token); every method takes a context.
type Client struct {
	base  string // server root, e.g. http://127.0.0.1:8080 (no trailing slash)
	token string
	hc    *http.Client
}

// HTTPClient is the http.Client every remote call shares (the CLI's
// `issues api` included). It refuses https→http redirects: Go forwards
// Authorization on same-host redirects even when the scheme downgrades,
// which would send the token unencrypted.
var HTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		for _, prev := range via {
			if prev.URL.Scheme == "https" && req.URL.Scheme == "http" {
				return fmt.Errorf("refusing https→http redirect to %s", req.URL)
			}
		}
		return nil
	},
}

// New returns a Client for the server's /api/v1. token is the Bearer
// credential — a shared env token or a per-user api_tokens value.
func New(server, token string) *Client {
	return &Client{
		base:  strings.TrimSuffix(server, "/") + "/api/v1",
		token: token,
		hc:    HTTPClient,
	}
}

// Error is a non-2xx API response with the server's error message.
type Error struct {
	Status  int
	Message string
	Fields  map[string]string
}

func (e *Error) Error() string {
	if len(e.Fields) > 0 {
		var fs []string
		for k, v := range e.Fields {
			fs = append(fs, k+": "+v)
		}
		return fmt.Sprintf("API error %d: %s (%s)", e.Status, e.Message, strings.Join(fs, ", "))
	}
	return fmt.Sprintf("API error %d: %s", e.Status, e.Message)
}

// do sends a JSON request and decodes a JSON response body into out
// (nil out skips decoding). Non-2xx becomes *Error.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body *bytes.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, req.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var e struct {
			Err struct {
				Message string            `json:"message"`
				Fields  map[string]string `json:"fields"`
			} `json:"error"`
		}
		ae := &Error{Status: resp.StatusCode, Message: resp.Status}
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Err.Message != "" {
			ae.Message = e.Err.Message
			ae.Fields = e.Err.Fields
		}
		return ae
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
