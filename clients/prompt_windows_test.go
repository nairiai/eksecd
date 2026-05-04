//go:build windows

package clients

import (
	"io"
	"testing"
)

func TestApplyPrompt_Windows(t *testing.T) {
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

			if args != nil {
				t.Errorf("expected nil positional args on Windows (prompt should travel via stdin), got %v", args)
			}
			if stdin == nil {
				t.Fatal("expected non-nil stdin reader on Windows")
			}

			read, err := io.ReadAll(stdin)
			if err != nil {
				t.Fatalf("failed to read stdin: %v", err)
			}
			if string(read) != tt.prompt {
				t.Errorf("stdin should contain the prompt verbatim, got %q want %q", string(read), tt.prompt)
			}
		})
	}
}
