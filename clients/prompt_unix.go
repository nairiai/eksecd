//go:build !windows

package clients

import "io"

// ApplyPrompt returns the positional argument(s) to append to an agent CLI
// command for prompt delivery, plus an optional stdin reader the caller
// should attach to the command before launching it.
//
// On Unix-like systems the prompt is delivered as a positional CLI
// argument and stdin is left untouched (returns nil reader).
func ApplyPrompt(prompt string) ([]string, io.Reader) {
	return []string{prompt}, nil
}
