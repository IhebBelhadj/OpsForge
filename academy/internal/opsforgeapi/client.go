// Package opsforgeapi is the adapter to OpsForge: its HTTP API (`opsforge api serve`; academy/contract/openapi.yaml
// and toolchain.schema.json, OpsForge's contract as pinned). Academy reads nothing of an OpsForge checkout: lab
// state, lab work (jobs), scenario-step checks, the learners' workspaces and their terminals all go through here.
// Every document carries `contract`, the major version: another one is refused; `GET /v1/version` also reports
// the minor: below MinMinor, OpsForge lacks something this platform uses.
package opsforgeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Contract is the major version of the contract this platform speaks.
const Contract = 1

// MinMinor is the oldest minor version of the contract this platform works with; MinRelease the OpsForge release
// that introduced it (named when OpsForge is too old). Raise both when the platform starts using an addition.
const (
	MinMinor   = 0
	MinRelease = "1.0.0"
)

// Version is what `GET /v1/version` reports.
type Version struct {
	Contract int    `json:"contract"`
	Minor    int    `json:"minor"`
	OpsForge string `json:"opsforge"`
}

// TooOldError is an OpsForge of this major whose minor lacks what the platform uses.
type TooOldError struct {
	Got Version
}

func (e *TooOldError) Error() string {
	return fmt.Sprintf("opsforge %s speaks contract %d.%d; academy needs %d.%d (opsforge %s or later)", e.Got.OpsForge,
		e.Got.Contract, e.Got.Minor, Contract, MinMinor, MinRelease)
}

// Problem is a failure OpsForge described (RFC 9457 fields), with the HTTP status it came with (0 for a failed
// job's problem).
type Problem struct {
	Status int    `json:"-"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func (p *Problem) Error() string { return fmt.Sprintf("opsforge: %s: %s", p.Title, p.Detail) }

// Slug is the problem's type without /problems/.
func (p *Problem) Slug() string { return strings.TrimPrefix(p.Type, "/problems/") }

// ContractError is a document of another contract version: OpsForge and the platform are out of step.
type ContractError struct {
	Got int
}

func (e *ContractError) Error() string {
	return fmt.Sprintf("opsforge speaks contract %d, academy speaks %d (install an opsforge release of contract %d)",
		e.Got, Contract, Contract)
}

var (
	// ErrInvalidDocument wraps a response that does not match toolchain.schema.json.
	ErrInvalidDocument = errors.New("document does not match toolchain.schema.json")
	// ErrJobLost is a job OpsForge no longer knows (it restarted, or kept it past an hour).
	ErrJobLost = errors.New("opsforge lost the job")
)

// Client calls OpsForge at a base URL with its token. Timeout bounds a call, LabTimeout a job (lab work waits for
// the cluster), unless the context ends sooner.
type Client struct {
	base       *url.URL
	token      Token
	http       *http.Client
	Timeout    time.Duration
	LabTimeout time.Duration
	// Wait is how long one poll of a running job waits for it on the server (?wait=, at most 60 s).
	Wait time.Duration
}

// Token returns OpsForge's API token for a call.
type Token func() (string, error)

// StaticToken is a token known in advance (tests).
func StaticToken(t string) Token { return func() (string, error) { return t, nil } }

// TokenFile reads the token from a file at every call, so a token copied (or rotated) after the start is used, and
// a missing one is an error of the calls (readyz says so) rather than of the start.
func TokenFile(path string) Token {
	return func() (string, error) {
		data, err := os.ReadFile(path) //nolint:gosec // the path is configuration
		if err != nil {
			return "", fmt.Errorf("opsforge token: %w (copy OpsForge's .opsforge/api-token there, mode 0600, or "+
				"set --opsforge-token-file)", err)
		}
		return strings.TrimSpace(string(data)), nil
	}
}

// New returns a Client for OpsForge at baseURL (http://127.0.0.1:8091).
func New(baseURL string, token Token, timeout, labTimeout time.Duration) (*Client, error) {
	u, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("opsforge url %q: not an http(s) URL", baseURL)
	}
	return &Client{base: u, token: token, http: &http.Client{}, Timeout: timeout, LabTimeout: labTimeout,
		Wait: 30 * time.Second}, nil
}

// URL is the base URL (for messages).
func (c *Client) URL() string { return c.base.String() }

func (c *Client) url(path string) string { return c.base.String() + path }

// do sends a request with the token; a response that is not one of want is returned as an error (a *Problem when
// OpsForge described it).
func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string,
	want ...int) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), body)
	if err != nil {
		return nil, err
	}
	token, err := c.token()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opsforge %s %s: %w", method, path, err)
	}
	for _, s := range want {
		if resp.StatusCode == s {
			return resp, nil
		}
	}
	defer func() { _ = resp.Body.Close() }()
	return nil, failure(method, path, resp)
}

func failure(method, path string, resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if kind, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); kind == "application/problem+json" {
		var doc struct {
			Contract *int `json:"contract"`
			Problem
		}
		if json.Unmarshal(data, &doc) == nil && doc.Contract != nil {
			if *doc.Contract != Contract {
				return &ContractError{Got: *doc.Contract}
			}
			doc.Status = resp.StatusCode
			return &doc.Problem
		}
	}
	return fmt.Errorf("opsforge %s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(data)))
}

// call sends `in` (nil: no body) as JSON and decodes the response document into out, after checking its contract
// version and, when def is set, toolchain.schema.json's $defs/<def>.
func (c *Client) call(ctx context.Context, method, path string, in any, def string, out any, want ...int) error {
	var body io.Reader
	contentType := ""
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("opsforge %s: input: %w", path, err)
		}
		body, contentType = bytes.NewReader(data), "application/json"
	}
	resp, err := c.do(ctx, method, path, body, contentType, want...)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("opsforge %s %s: %w", method, path, err)
	}
	return decode(path, data, def, out)
}

func decode(what string, data []byte, def string, out any) error {
	var head struct {
		Contract *int `json:"contract"`
	}
	if err := json.Unmarshal(data, &head); err != nil || head.Contract == nil {
		return fmt.Errorf("opsforge %s: not a JSON document with a contract version", what)
	}
	if *head.Contract != Contract {
		return &ContractError{Got: *head.Contract}
	}
	if def != "" {
		if err := validate(data, "toolchain.schema.json#/$defs/"+def); err != nil {
			return fmt.Errorf("opsforge %s: %w: %w", what, ErrInvalidDocument, err)
		}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("opsforge %s: %w", what, err)
	}
	return nil
}

// Version is OpsForge's contract version and release (no token needed).
func (c *Client) Version(ctx context.Context) (Version, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	var v Version
	return v, c.call(ctx, http.MethodGet, "/v1/version", nil, "versionOutput", &v, http.StatusOK)
}

// Ready reports whether OpsForge answers, speaks this contract at MinMinor or later, and takes the token (readyz).
func (c *Client) Ready(ctx context.Context) error {
	v, err := c.Version(ctx)
	if err != nil {
		return err
	}
	if v.Minor < MinMinor {
		return &TooOldError{Got: v}
	}
	_, err = c.State(ctx) // the token: version is the one public route
	return err
}

// jsonBody is v as a request body (v is the platform's own document: it always marshals).
func jsonBody(v any) io.Reader {
	data, _ := json.Marshal(v)
	return bytes.NewReader(data)
}
