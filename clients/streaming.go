package clients

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// CommandError wraps a command execution error with its captured output.
// Client packages convert this to core.ErrClaudeCommandErr.
type CommandError struct {
	Err    error
	Output string
}

func (e *CommandError) Error() string {
	return e.Err.Error()
}

func (e *CommandError) Unwrap() error {
	return e.Err
}

// lineStartsWebFetch reports whether a Claude Code stream-json line represents an
// assistant message that kicks off a WebFetch tool call. Claude Code emits these
// as compact JSON, e.g. {"type":"tool_use","name":"WebFetch",...}. A hung WebFetch
// produces no further output until it (never) returns, so this marks the point
// after which the stall watchdog starts counting.
func lineStartsWebFetch(line []byte) bool {
	return bytes.Contains(line, []byte(`"type":"tool_use"`)) &&
		bytes.Contains(line, []byte(`"name":"WebFetch"`))
}

// RunCommandStreaming executes a command, reading stdout line-by-line and calling onLine
// for each non-empty line. It accumulates the full output and returns it on success.
// This replaces cmd.CombinedOutput() to enable real-time progress streaming.
//
// It additionally runs a "WebFetch stall" watchdog: whenever a stream line starts a
// WebFetch tool call, a timer is armed for WebFetchStallTimeout(). If no further
// output arrives before it fires, the agent's whole process group is killed (via the
// same cmd.Cancel machinery used for context timeouts). This bounds the hung-WebFetch
// failure mode that Claude Code does not guard against — without it, a stuck fetch
// silently stalls the entire turn until the far larger SessionTimeout fires, and the
// user's message gets no response. Any subsequent stream line disarms the watchdog.
func RunCommandStreaming(ctx context.Context, cmd *exec.Cmd, onLine ProgressCallback) (string, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start command: %w", err)
	}

	stallTimeout := WebFetchStallTimeout()

	// Watchdog state guarded by mu. armedAt is the zero value when disarmed;
	// otherwise it is the time the pending WebFetch tool call was observed.
	var (
		mu      sync.Mutex
		armedAt time.Time
		stalled bool
	)
	stopWatch := make(chan struct{})
	if stallTimeout > 0 {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopWatch:
					return
				case <-ticker.C:
					mu.Lock()
					hung := !armedAt.IsZero() && time.Since(armedAt) >= stallTimeout
					if hung {
						stalled = true
						armedAt = time.Time{}
					}
					mu.Unlock()
					if hung {
						// Kill the whole process group; cmd.WaitDelay guarantees
						// Wait() below won't block on an orphaned pipe. This unblocks
						// the ReadBytes call in the loop below.
						if cmd.Cancel != nil {
							_ = cmd.Cancel()
						} else if cmd.Process != nil {
							_ = cmd.Process.Kill()
						}
						return
					}
				}
			}
		}()
	}

	var fullOutput strings.Builder
	reader := bufio.NewReader(stdout)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			fullOutput.Write(line)
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) > 0 {
				if onLine != nil {
					onLine(trimmed)
				}
				if stallTimeout > 0 {
					// Any line disarms a pending WebFetch; a line that itself starts
					// a WebFetch (re)arms the watchdog from now.
					mu.Lock()
					if lineStartsWebFetch(trimmed) {
						armedAt = time.Now()
					} else {
						armedAt = time.Time{}
					}
					mu.Unlock()
				}
			}
		}
		if err != nil {
			break
		}
	}
	close(stopWatch)

	waitErr := cmd.Wait()

	mu.Lock()
	wasStalled := stalled
	mu.Unlock()
	if wasStalled {
		return "", &CommandError{
			Err:    fmt.Errorf("WebFetch stalled: no stream output for %s — a WebFetch tool call appears to have hung", stallTimeout),
			Output: fullOutput.String(),
		}
	}

	if waitErr != nil {
		return "", &CommandError{
			Err:    waitErr,
			Output: fullOutput.String(),
		}
	}

	return strings.TrimSpace(fullOutput.String()), nil
}
