package handlers

import (
	"nairid/models"
	"testing"
)

func TestStripAccessTokenFromURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "URL with x-access-token",
			input:    "https://x-access-token:ghs_1234567890abcdefghijklmnop@github.com/owner/repo",
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "URL without x-access-token",
			input:    "https://github.com/owner/repo",
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "Empty URL",
			input:    "",
			expected: "",
		},
		{
			name:     "URL with x-access-token and path",
			input:    "https://x-access-token:token123@github.com/owner/repo/commit/abc123",
			expected: "https://github.com/owner/repo/commit/abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripAccessTokenFromURL(tt.input)
			if result != tt.expected {
				t.Errorf("stripAccessTokenFromURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestStripAccessTokenFromURL_GitLabAndUserinfo(t *testing.T) {
	tests := []struct {
		name, input, expected string
	}{
		{
			name:     "GitLab oauth2 userinfo",
			input:    "https://oauth2:glpat-xxxx@gitlab.example.com/group/sub/repo",
			expected: "https://gitlab.example.com/group/sub/repo",
		},
		{
			name:     "GitLab oauth2 with path containing @",
			input:    "https://oauth2:tok@gitlab.example.com/group/repo/-/commit/abc",
			expected: "https://gitlab.example.com/group/repo/-/commit/abc",
		},
		{
			name:     "self-hosted host with port",
			input:    "https://oauth2:tok@code.acme.com:8443/group/repo",
			expected: "https://code.acme.com:8443/group/repo",
		},
		{
			name:     "no scheme returns unchanged",
			input:    "git@github.com:owner/repo.git",
			expected: "git@github.com:owner/repo.git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripAccessTokenFromURL(tt.input); got != tt.expected {
				t.Errorf("stripAccessTokenFromURL(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractPRNumber(t *testing.T) {
	tests := []struct {
		name, input, expected string
	}{
		{"github pull URL", "https://github.com/owner/repo/pull/1234", "#1234"},
		{"gitlab MR URL", "https://gitlab.example.com/group/sub/repo/-/merge_requests/56", "#56"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractPRNumber(tt.input); got != tt.expected {
				t.Errorf("extractPRNumber(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestPrependSenderMetadata(t *testing.T) {
	slackPlatform := models.PlatformSlack

	tests := []struct {
		name     string
		message  string
		metadata *models.UserMetadata
		expected string
	}{
		{
			name:     "nil metadata returns original message",
			message:  "hello world",
			metadata: nil,
			expected: "hello world",
		},
		{
			name:     "empty metadata returns original message",
			message:  "hello world",
			metadata: &models.UserMetadata{},
			expected: "hello world",
		},
		{
			name:    "name and platform prepends sender header",
			message: "review my PR",
			metadata: &models.UserMetadata{
				Name:     strPtr("Alice"),
				Platform: &slackPlatform,
			},
			expected: "[Sender: Alice via slack]\n\nreview my PR",
		},
		{
			name:    "name only prepends sender header",
			message: "check this",
			metadata: &models.UserMetadata{
				Name: strPtr("Bob"),
			},
			expected: "[Sender: Bob]\n\ncheck this",
		},
		{
			name:    "full prod metadata with slack mrkdwn email",
			message: "deploy the service",
			metadata: &models.UserMetadata{
				ID:       strPtr("U08S1TQ0QLR"),
				Name:     strPtr("Pres"),
				Email:    strPtr("<mailto:pmihaylov95@gmail.com|pmihaylov95@gmail.com>"),
				Platform: &slackPlatform,
			},
			expected: "[Sender: Pres (pmihaylov95@gmail.com) via slack]\n\ndeploy the service",
		},
		{
			name:    "email and platform without name",
			message: "hello",
			metadata: &models.UserMetadata{
				Email:    strPtr("user@example.com"),
				Platform: &slackPlatform,
			},
			expected: "[Sender: (user@example.com) via slack]\n\nhello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prependSenderMetadata(tt.message, tt.metadata)
			if result != tt.expected {
				t.Errorf("prependSenderMetadata() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}
