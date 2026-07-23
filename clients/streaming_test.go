package clients

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunCommandStreaming_Success(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "echo", "hello\nworld")

	var lines []string
	output, err := RunCommandStreaming(context.Background(), cmd, func(line []byte) {
		lines = append(lines, string(line))
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected output to contain 'hello', got: %q", output)
	}
	if len(lines) == 0 {
		t.Error("expected at least one callback invocation")
	}
}

func TestRunCommandStreaming_MultipleLines(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "printf", "line1\nline2\nline3\n")

	var lines []string
	output, err := RunCommandStreaming(context.Background(), cmd, func(line []byte) {
		lines = append(lines, string(line))
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 callback invocations, got %d: %v", len(lines), lines)
	}
	if output != "line1\nline2\nline3" {
		t.Errorf("unexpected full output: %q", output)
	}
}

func TestRunCommandStreaming_NonZeroExit(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "echo partial output; exit 1")

	var lines []string
	output, err := RunCommandStreaming(context.Background(), cmd, func(line []byte) {
		lines = append(lines, string(line))
	})

	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	cmdErr, ok := err.(*CommandError)
	if !ok {
		t.Fatalf("expected *CommandError, got %T", err)
	}
	if !strings.Contains(cmdErr.Output, "partial output") {
		t.Errorf("expected CommandError.Output to contain partial output, got: %q", cmdErr.Output)
	}
	if output != "" {
		t.Errorf("expected empty output on error, got: %q", output)
	}
	if len(lines) != 1 {
		t.Errorf("expected 1 callback invocation before failure, got %d", len(lines))
	}
}

func TestRunCommandStreaming_EmptyLinesSkipped(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "printf", "a\n\n\nb\n")

	var lines []string
	_, err := RunCommandStreaming(context.Background(), cmd, func(line []byte) {
		lines = append(lines, string(line))
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 2 {
		t.Errorf("expected 2 callbacks (empty lines skipped), got %d: %v", len(lines), lines)
	}
}

func TestRunCommandStreaming_NilCallback(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "echo", "hello")

	output, err := RunCommandStreaming(context.Background(), cmd, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected output to contain 'hello', got: %q", output)
	}
}

func TestRunCommandStreaming_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	cmd := exec.CommandContext(ctx, "sleep", "10")
	_, err := RunCommandStreaming(ctx, cmd, nil)

	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestRunCommandStreaming_TimeoutKillsProcessTree(t *testing.T) {
	// Simulate the actual bug: a parent process spawns a child that hangs.
	// Without process group kill + WaitDelay, RunCommandStreaming blocks forever
	// because the child keeps stdout open.
	original := os.Getenv("AGENT_EXEC_USER")
	defer func() { _ = os.Setenv("AGENT_EXEC_USER", original) }()
	_ = os.Unsetenv("AGENT_EXEC_USER")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// sh spawns a background child (sleep 300) that holds stdout open,
	// then the parent also hangs. This mimics the opencode hang scenario.
	cmd := BuildAgentCommandWithContext(ctx, "sh", "-c", "sleep 300 & wait")

	start := time.Now()
	_, err := RunCommandStreaming(ctx, cmd, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from timeout")
	}

	// Should complete within a reasonable time (timeout + WaitDelay + margin).
	// If process group kill is broken, this would block for 300 seconds.
	maxExpected := 500*time.Millisecond + WaitDelayAfterKill + 5*time.Second
	if elapsed > maxExpected {
		t.Errorf("RunCommandStreaming took %v, expected under %v (process group kill may not be working)", elapsed, maxExpected)
	}
}

func TestLineStartsWebFetch(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{"webfetch tool_use", `{"type":"tool_use","name":"WebFetch","input":{"url":"https://x.com"}}`, true},
		{"bash tool_use", `{"type":"tool_use","name":"Bash","input":{"command":"ls"}}`, false},
		{"assistant text mentioning WebFetch", `{"type":"text","text":"I will use WebFetch now"}`, false},
		{"webfetch name without tool_use type", `{"type":"text","name":"WebFetch"}`, false},
		{"result line", `{"type":"result","is_error":false}`, false},
	}
	for _, tc := range cases {
		if got := lineStartsWebFetch([]byte(tc.line)); got != tc.want {
			t.Errorf("%s: lineStartsWebFetch(%q) = %v, want %v", tc.name, tc.line, got, tc.want)
		}
	}
}

// TestRunCommandStreaming_WebFetchStallKillsHungProcess simulates the production
// incident: the agent emits a WebFetch tool_use event and then hangs forever. The
// stall watchdog must kill the process group quickly and return a stall CommandError
// instead of blocking until the (much larger) session timeout.
func TestRunCommandStreaming_WebFetchStallKillsHungProcess(t *testing.T) {
	original := os.Getenv("AGENT_EXEC_USER")
	defer func() { _ = os.Setenv("AGENT_EXEC_USER", original) }()
	_ = os.Unsetenv("AGENT_EXEC_USER")
	t.Setenv("NAIRI_WEBFETCH_STALL_MS", "1000")

	// Generous ctx timeout so we prove the *watchdog* (not the ctx) fired.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	webfetch := `{"type":"tool_use","name":"WebFetch","input":{"url":"https://example.com"}}`
	cmd := BuildAgentCommandWithContext(ctx, "sh", "-c",
		"printf '%s\\n' '"+webfetch+"'; sleep 300 & wait")

	start := time.Now()
	_, err := RunCommandStreaming(ctx, cmd, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected stall error for hung WebFetch")
	}
	cmdErr, ok := err.(*CommandError)
	if !ok {
		t.Fatalf("expected *CommandError, got %T", err)
	}
	if !strings.Contains(cmdErr.Error(), "WebFetch stalled") {
		t.Errorf("expected WebFetch stall error, got: %v", cmdErr.Error())
	}
	// Watchdog (1s) + tick granularity + WaitDelay should be well under the 60s ctx.
	if elapsed > WaitDelayAfterKill+10*time.Second {
		t.Errorf("RunCommandStreaming took %v, expected the watchdog to kill much sooner", elapsed)
	}
}

// TestRunCommandStreaming_NonWebFetchNotKilled proves the watchdog is WebFetch-
// specific: a non-WebFetch tool call that runs longer than the stall timeout must
// NOT be killed, so ordinary long-running tools (e.g. Bash builds) are unaffected.
func TestRunCommandStreaming_NonWebFetchNotKilled(t *testing.T) {
	original := os.Getenv("AGENT_EXEC_USER")
	defer func() { _ = os.Setenv("AGENT_EXEC_USER", original) }()
	_ = os.Unsetenv("AGENT_EXEC_USER")
	t.Setenv("NAIRI_WEBFETCH_STALL_MS", "1000")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bash := `{"type":"tool_use","name":"Bash","input":{"command":"sleep"}}`
	cmd := BuildAgentCommandWithContext(ctx, "sh", "-c",
		"printf '%s\\n' '"+bash+"'; sleep 3; printf 'done\\n'")

	out, err := RunCommandStreaming(ctx, cmd, nil)
	if err != nil {
		t.Fatalf("non-WebFetch tool should not be killed by the watchdog, got error: %v", err)
	}
	if !strings.Contains(out, "done") {
		t.Errorf("expected output to contain 'done', got: %q", out)
	}
}

// TestRunCommandStreaming_WebFetchDisarmedByOutput ensures a WebFetch that keeps
// producing output (or completes) before the stall window is not killed.
func TestRunCommandStreaming_WebFetchDisarmedByOutput(t *testing.T) {
	t.Setenv("NAIRI_WEBFETCH_STALL_MS", "1000")

	webfetch := `{"type":"tool_use","name":"WebFetch","input":{"url":"https://example.com"}}`
	result := `{"type":"result","is_error":false}`
	cmd := exec.CommandContext(context.Background(), "sh", "-c",
		"printf '%s\\n' '"+webfetch+"'; printf '%s\\n' '"+result+"'")

	out, err := RunCommandStreaming(context.Background(), cmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "result") {
		t.Errorf("expected output to contain result line, got: %q", out)
	}
}
