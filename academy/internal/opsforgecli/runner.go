// Package opsforgecli is the adapter to the lab toolchain: `opsforge api <command> --json`, one JSON document on
// stdin and one on stdout, a problem document on stderr when it fails (academy/contract/README.md, OpsForge's
// contract as pinned). Every document carries `contract`, the major version: another one is refused. `api version`
// also reports the minor: below MinMinor, the toolchain lacks something this platform uses.
package opsforgecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Contract is the major version of the toolchain contract this platform speaks.
const Contract = 1

// MinMinor is the oldest minor version of the contract this platform works with; MinRelease the OpsForge release
// that introduced it (named when the toolchain is too old). Raise both when the platform starts using an addition.
const (
	MinMinor   = 0
	MinRelease = "1.0.0"
)

// Version is what `api version` reports.
type Version struct {
	Contract int    `json:"contract"`
	Minor    int    `json:"minor"`
	OpsForge string `json:"opsforge"`
}

// TooOldError is a toolchain of this major whose minor lacks what the platform uses.
type TooOldError struct {
	Got Version
}

func (e *TooOldError) Error() string {
	return fmt.Sprintf("opsforge %s speaks contract %d.%d; academy needs %d.%d (opsforge %s or later)", e.Got.OpsForge,
		e.Got.Contract, e.Got.Minor, Contract, MinMinor, MinRelease)
}

// Problem is a failure the toolchain described (RFC 9457 fields).
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func (p *Problem) Error() string { return fmt.Sprintf("toolchain: %s: %s", p.Title, p.Detail) }

// ContractError is a document of another contract version: the toolchain and the platform are out of step.
type ContractError struct {
	Got int
}

func (e *ContractError) Error() string {
	return fmt.Sprintf("toolchain speaks contract %d, academy speaks %d (install an opsforge release of contract %d)",
		e.Got, Contract, Contract)
}

// Runner calls the CLI at Path from the repository Root. Timeout bounds a call, LabTimeout one that runs steps on
// the lab (they wait for the cluster), unless the context ends sooner.
type Runner struct {
	Path       string
	Root       string
	Timeout    time.Duration
	LabTimeout time.Duration
}

// New returns a Runner.
func New(path, root string, timeout, labTimeout time.Duration) *Runner {
	return &Runner{Path: path, Root: root, Timeout: timeout, LabTimeout: labTimeout}
}

// Call executes `api <command> --json` with `in` (nil: no input) as stdin and decodes stdout into out.
func (r *Runner) Call(ctx context.Context, command string, in, out any) error {
	return r.call(ctx, r.Timeout, command, in, out)
}

func (r *Runner) call(ctx context.Context, timeout time.Duration, command string, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// The CLI path is configuration and the command one of the contract's: no learner input reaches argv
	cmd := exec.CommandContext(ctx, r.Path, "api", command, "--json") //nolint:gosec // see above
	cmd.Dir = r.Root
	cmd.WaitDelay = time.Second // a child that keeps the pipes open must not outlive the timeout
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("toolchain %s: input: %w", command, err)
		}
		cmd.Stdin = bytes.NewReader(data)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("toolchain %s: %w", command, ctx.Err())
		}
		return failure(command, err, stderr.Bytes())
	}
	return decode(command, stdout.Bytes(), out)
}

func failure(command string, err error, stderr []byte) error {
	var doc struct {
		Contract *int `json:"contract"`
		Problem
	}
	if json.Unmarshal(stderr, &doc) == nil && doc.Contract != nil && doc.Type != "" {
		if *doc.Contract != Contract {
			return &ContractError{Got: *doc.Contract}
		}
		return &doc.Problem
	}
	return fmt.Errorf("toolchain %s: %w: %s", command, err, strings.TrimSpace(string(stderr)))
}

func decode(command string, stdout []byte, out any) error {
	var head struct {
		Contract *int `json:"contract"`
	}
	if err := json.Unmarshal(stdout, &head); err != nil || head.Contract == nil {
		return fmt.Errorf("toolchain %s: stdout is not a JSON document with a contract version", command)
	}
	if *head.Contract != Contract {
		return &ContractError{Got: *head.Contract}
	}
	if err := json.Unmarshal(stdout, out); err != nil {
		return fmt.Errorf("toolchain %s: %w", command, err)
	}
	return nil
}

// Version is the toolchain's contract version and release.
func (r *Runner) Version(ctx context.Context) (Version, error) {
	var v Version
	return v, r.Call(ctx, "version", nil, &v)
}

// Ready reports whether the toolchain runs and speaks this contract at MinMinor or later (readyz).
func (r *Runner) Ready(ctx context.Context) error {
	v, err := r.Version(ctx)
	if err != nil {
		return err
	}
	if v.Minor < MinMinor {
		return &TooOldError{Got: v}
	}
	return nil
}

// ErrInvalidDocument wraps a run, probe, heal or validate-steps document that does not match toolchain.schema.json.
var ErrInvalidDocument = errors.New("document does not match toolchain.schema.json")
