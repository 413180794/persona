// Package sourcevalidate validates untrusted module syntax only in a disposable,
// independently limited native process. It never evaluates JavaScript.
package sourcevalidate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/extension/sandbox"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/osproc"
)

const Command = "__source-validate"
const Most = 16 << 20
const memoryMB = 128
const cpuPercent = 50
const deadline = 5 * time.Second

var ErrUnavailable = i18n.ErrEnglish("extension-source.validator_unavailable")
var ErrInvalid = i18n.ErrEnglish("extension-source.validator_rejected")
var ErrBusy = i18n.ErrEnglish("extension-source.validator_busy")
var ErrStopped = i18n.ErrEnglish("extension-source.validator_stopped")

// Three admitted requests, one live validator. Queue admission never waits and
// queued requests share the external deadline. No goroutine is spawned to queue.
var admitted = make(chan struct{}, 3)
var running = make(chan struct{}, 1)

// Validate streams bounded source to the child. digest binds that exact stream to
// the manager's preceding signature/hash check, even if a file changes meanwhile.
func Validate(ctx context.Context, input io.Reader, digest string) error {
	self, err := os.Executable()
	if err != nil {
		return ErrUnavailable
	}
	return validate(ctx, input, digest, func(ctx context.Context) (*exec.Cmd, func(), error) {
		return sandbox.ValidationProcess(ctx, self, Command, []string{digest}, memoryMB, cpuPercent)
	})
}

type launch func(context.Context) (*exec.Cmd, func(), error)

func validate(ctx context.Context, input io.Reader, digest string, prepare launch) error {
	if !validDigest(digest) {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	select {
	case admitted <- struct{}{}:
		defer func() { <-admitted }()
	default:
		return ErrBusy
	}
	select {
	case running <- struct{}{}:
		defer func() { <-running }()
	case <-ctx.Done():
		return ErrStopped
	}
	if ctx.Err() != nil {
		return ErrStopped
	}
	cmd, dispose, err := prepare(ctx)
	if err != nil || cmd == nil || dispose == nil {
		if ctx.Err() != nil {
			return ErrStopped
		}
		return ErrUnavailable
	}
	defer dispose()
	// Only bounded artifacts/readers created by the manager are passed here.
	// No attacker-controlled network reader, environment, argv, or working dir.
	cmd.Stdin = io.LimitReader(input, Most+1)
	var result boundedResult
	cmd.Stdout = &result
	if cmd.Stderr == nil {
		cmd.Stderr = io.Discard
	}
	cmd.SysProcAttr = osproc.GroupAttr()
	cmd.Cancel = func() error { return osproc.StopGroup(cmd.Process.Pid, true) }
	cmd.WaitDelay = 100 * time.Millisecond
	if err := cmd.Start(); err != nil {
		if ctx.Err() != nil {
			return ErrStopped
		}
		return ErrUnavailable
	}
	err = cmd.Wait()
	// Covers both cancellation and an unexpectedly exiting wrapper. Kill the
	// remaining group before returning; bwrap also kills its private PID namespace.
	_ = osproc.StopGroup(cmd.Process.Pid, true)
	if ctx.Err() != nil {
		return ErrStopped
	}
	if err != nil || result.overflow {
		return ErrUnavailable
	}
	switch string(result.bytes[:result.n]) {
	case "ok\n":
		return nil
	case "reject\n":
		return ErrInvalid
	default:
		return ErrUnavailable
	}
}

// Fixed-size, never a log or unbounded bytes.Buffer. Excess output is discarded
// and fails closed. WaitDelay/deadline also bound inherited output pipe lifetimes.
type boundedResult struct {
	bytes    [16]byte
	n        int
	overflow bool
}

func (r *boundedResult) Write(b []byte) (int, error) {
	n := copy(r.bytes[r.n:], b)
	r.n += n
	r.overflow = r.overflow || n != len(b)
	return len(b), nil
}
func validDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && strings.ToLower(s) == s
}
