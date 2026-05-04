//go:build !windows

package clients

import (
	"testing"
)

func TestApplyPrompt_Unix(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
	}{
		{name: "empty prompt", prompt: ""},
		{name: "single line", prompt: "hello"},
		{
			name:   "slack sender header (multi-line)",
			prompt: "[Sender: Pres (pmihaylov95@gmail.com) via slack]\n\n@nairi tell me what day we are today",
		},
		{name: "code block with newlines", prompt: "fix this:\n```go\nfmt.Println(\"hi\")\n```"},
		{name: "shell metacharacters", prompt: "do `rm -rf /tmp/x` & ls | grep foo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, stdin := ApplyPrompt(tt.prompt)

			if stdin != nil {
				t.Errorf("expected nil stdin reader on Unix, got %T", stdin)
			}
			if len(args) != 1 {
				t.Fatalf("expected exactly 1 positional arg, got %d: %v", len(args), args)
			}
			if args[0] != tt.prompt {
				t.Errorf("expected positional arg to be the prompt verbatim, got %q want %q", args[0], tt.prompt)
			}
		})
	}
}
