//go:build windows

package clients

import (
	"io"
	"strings"
)

// ApplyPrompt returns the positional argument(s) to append to an agent CLI
// command for prompt delivery, plus an optional stdin reader the caller
// should attach to the command before launching it.
//
// On Windows the prompt is delivered via stdin instead of as a CLI
// argument. Agent CLIs (claude, codex, opencode, cursor-agent) are
// typically installed as .cmd batch shims via npm. cmd.exe truncates
// arguments at embedded newlines when expanding %* inside the shim, so
// a multi-line prompt (e.g. one carrying the "[Sender: ...]\n\n<msg>"
// header for Slack/Discord messages, or any user-supplied multi-line
// content) reaches the agent with everything after the first newline
// stripped off, leaving the agent with what looks like an empty input.
//
// Stdin pipes go directly from the parent process into the child via
// the Windows kernel and bypass cmd.exe's argument parsing entirely,
// so newlines and other shell metacharacters are preserved verbatim.
func ApplyPrompt(prompt string) ([]string, io.Reader) {
	return nil, strings.NewReader(prompt)
}
