package forge

import (
	"fmt"
	"testing"
)

// ============ GitHub classifier (lifted verbatim from clients/git_test.go) ============

func TestGitHub_isRateLimit_NilError(t *testing.T) {
	if gitHubClassifier().isRateLimit(nil, "") {
		t.Error("Expected false for nil error")
	}
}

func TestGitHub_isRateLimit_RateLimitErrors(t *testing.T) {
	cls := gitHubClassifier()
	tests := []struct{ name, err, output string }{
		{"graphql rate limit in output", "exit status 1", "GraphQL: API rate limit already exceeded for installation ID 92312766."},
		{"rate limit in error", "API rate limit exceeded", ""},
		{"secondary rate limit in output", "exit status 1", "You have exceeded a secondary rate limit"},
		{"abuse detection in output", "exit status 1", "You have triggered an abuse detection mechanism"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !cls.isRateLimit(fmt.Errorf("%s", tc.err), tc.output) {
				t.Errorf("Expected rate limit for err=%q output=%q", tc.err, tc.output)
			}
		})
	}
}

func TestGitHub_isRateLimit_NonRateLimitErrors(t *testing.T) {
	cls := gitHubClassifier()
	tests := []struct{ name, err, output string }{
		{"timeout", "connection timeout", ""},
		{"generic error", "something went wrong", ""},
		{"not found", "exit status 1", "not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if cls.isRateLimit(fmt.Errorf("%s", tc.err), tc.output) {
				t.Errorf("Expected non-rate-limit for err=%q output=%q", tc.err, tc.output)
			}
		})
	}
}

func TestGitHub_isRecoverable_NilError(t *testing.T) {
	if gitHubClassifier().isRecoverable(nil, "") {
		t.Error("Expected false for nil error")
	}
}

func TestGitHub_isRecoverable_NetworkErrors(t *testing.T) {
	cls := gitHubClassifier()
	tests := []struct{ name, err, output string }{
		{"timeout in error", "connection timeout", ""},
		{"dial tcp in error", "dial tcp 1.2.3.4:443: i/o timeout", ""},
		{"context deadline in error", "context deadline exceeded", ""},
		{"timeout in output", "exit status 1", "i/o timeout"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !cls.isRecoverable(fmt.Errorf("%s", tc.err), tc.output) {
				t.Errorf("Expected recoverable for err=%q output=%q", tc.err, tc.output)
			}
		})
	}
}

func TestGitHub_isRecoverable_RateLimitErrors(t *testing.T) {
	cls := gitHubClassifier()
	tests := []struct{ name, err, output string }{
		{"graphql rate limit in output", "exit status 1", "GraphQL: API rate limit already exceeded for installation ID 92312766."},
		{"rate limit in error", "API rate limit exceeded", ""},
		{"secondary rate limit in output", "exit status 1", "You have exceeded a secondary rate limit"},
		{"abuse detection in output", "exit status 1", "You have triggered an abuse detection mechanism"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !cls.isRecoverable(fmt.Errorf("%s", tc.err), tc.output) {
				t.Errorf("Expected recoverable for err=%q output=%q", tc.err, tc.output)
			}
		})
	}
}

func TestGitHub_isRecoverable_NonRecoverableErrors(t *testing.T) {
	cls := gitHubClassifier()
	tests := []struct{ name, err, output string }{
		{"generic error", "something went wrong", ""},
		{"not found", "exit status 1", "not found"},
		{"permission denied", "exit status 1", "permission denied"},
		{"invalid branch", "exit status 128", "fatal: invalid reference"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if cls.isRecoverable(fmt.Errorf("%s", tc.err), tc.output) {
				t.Errorf("Expected non-recoverable for err=%q output=%q", tc.err, tc.output)
			}
		})
	}
}

// ============ GitLab classifier ============

func TestGitLab_isRecoverable_TransientErrors(t *testing.T) {
	cls := gitLabClassifier()
	tests := []struct{ name, err, output string }{
		{"429 too many requests", "exit status 1", "429 Too Many Requests"},
		{"rate limit phrase", "exit status 1", "You have exceeded a secondary rate limit. Please retry later"},
		{"503 service unavailable", "exit status 1", "503 Service Unavailable"},
		{"502 bad gateway", "exit status 1", "502 Bad Gateway"},
		{"504 gateway timeout", "exit status 1", "504 Gateway Timeout"},
		{"read timed out", "exit status 1", "Read timed out"},
		{"network dial", "dial tcp 10.0.0.1:443: i/o timeout", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !cls.isRecoverable(fmt.Errorf("%s", tc.err), tc.output) {
				t.Errorf("Expected recoverable for err=%q output=%q", tc.err, tc.output)
			}
		})
	}
}

func TestGitLab_isRecoverable_NonRecoverableErrors(t *testing.T) {
	cls := gitLabClassifier()
	// 401/auth and ordinary failures must NOT be retried.
	tests := []struct{ name, err, output string }{
		{"401 unauthorized", "exit status 1", "401 Unauthorized"},
		{"permission denied", "exit status 1", "403 Forbidden: insufficient permissions"},
		{"not found", "exit status 1", "404 Project Not Found"},
		{"generic", "something went wrong", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if cls.isRecoverable(fmt.Errorf("%s", tc.err), tc.output) {
				t.Errorf("Expected non-recoverable for err=%q output=%q", tc.err, tc.output)
			}
		})
	}
}
