package forge

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"nairid/core/log"
)

// GitHubProvider drives merge/pull requests through the GitHub CLI (`gh`).
type GitHubProvider struct {
	cls classifier
}

// NewGitHubProvider returns a GitHub provider. GitHub is always hosted at
// github.com for nairid's purposes.
func NewGitHubProvider() *GitHubProvider {
	return &GitHubProvider{cls: gitHubClassifier()}
}

// gitHubClassifier holds the error patterns that mark a `gh` failure as a rate
// limit or otherwise transient/recoverable error.
func gitHubClassifier() classifier {
	return classifier{
		rateLimitPatterns: []string{
			"rate limit",
			"secondary rate limit",
			"abuse detection",
		},
		recoverablePatterns: []string{
			"timeout",
			"i/o timeout",
			"connection timeout",
			"dial tcp",
			"context deadline exceeded",
		},
	}
}

func (p *GitHubProvider) Name() string    { return "github" }
func (p *GitHubProvider) CLIName() string { return "gh" }

func (p *GitHubProvider) run(workDir, operationName string, args ...string) ([]byte, error) {
	cmd := exec.Command("gh", args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	return executeWithRetry(cmd, workDir, operationName, "GitHub", p.cls)
}

func (p *GitHubProvider) IsCLIAvailable(workDir string) error {
	log.Info("📋 Starting to check GitHub CLI availability")

	cmd := exec.Command("gh", "--version")
	if workDir != "" {
		cmd.Dir = workDir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Error("❌ GitHub CLI not found: %v\nOutput: %s", err, string(output))
		return fmt.Errorf("github cli (gh) not found: %w\nOutput: %s", err, string(output))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd = exec.CommandContext(ctx, "gh", "auth", "status")
	if workDir != "" {
		cmd.Dir = workDir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Error("❌ GitHub CLI not authenticated: %v\nOutput: %s", err, string(output))
		return fmt.Errorf("github cli not authenticated (run 'gh auth login'): %w\nOutput: %s", err, string(output))
	}

	log.Info("✅ GitHub CLI is available and authenticated")
	return nil
}

func (p *GitHubProvider) CreatePR(workDir, title, body, baseBranch string) (string, error) {
	output, err := p.run(workDir, "create pull request",
		"pr", "create", "--title", title, "--body", body, "--base", baseBranch)
	if err != nil {
		log.Error("❌ GitHub PR creation failed for title '%s': %v\nOutput: %s", title, err, string(output))
		return "", fmt.Errorf("github pr creation failed: %w\nOutput: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

func (p *GitHubProvider) GetPRURL(workDir, branchName string) (string, error) {
	output, err := p.run(workDir, "get PR URL",
		"pr", "view", branchName, "--json", "url", "--jq", ".url")
	if err != nil {
		return "", fmt.Errorf("failed to get PR URL: %w\nOutput: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

func (p *GitHubProvider) HasExistingPR(workDir, branchName string) (bool, error) {
	output, err := p.run(workDir, "check existing PR",
		"pr", "list", "--head", branchName, "--json", "number")
	if err != nil {
		return false, fmt.Errorf("failed to check for existing PR: %w\nOutput: %s", err, string(output))
	}
	outputStr := strings.TrimSpace(string(output))
	return outputStr != "[]" && outputStr != "", nil
}

func (p *GitHubProvider) GetPRTitle(workDir, branchName string) (string, error) {
	output, err := p.run(workDir, "get PR title",
		"pr", "view", branchName, "--json", "title", "--jq", ".title")
	if err != nil {
		return "", fmt.Errorf("failed to get PR title: %w\nOutput: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

func (p *GitHubProvider) GetPRDescription(workDir, branchName string) (string, error) {
	output, err := p.run(workDir, "get PR description",
		"pr", "view", branchName, "--json", "body", "--jq", ".body")
	if err != nil {
		return "", fmt.Errorf("failed to get PR description: %w\nOutput: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

func (p *GitHubProvider) UpdatePRTitle(workDir, branchName, newTitle string) error {
	output, err := p.run(workDir, "update PR title",
		"pr", "edit", branchName, "--title", newTitle)
	if err != nil {
		return fmt.Errorf("failed to update PR title: %w\nOutput: %s", err, string(output))
	}
	return nil
}

func (p *GitHubProvider) UpdatePRDescription(workDir, branchName, newBody string) error {
	output, err := p.run(workDir, "update PR description",
		"pr", "edit", branchName, "--body", newBody)
	if err != nil {
		return fmt.Errorf("failed to update PR description: %w\nOutput: %s", err, string(output))
	}
	return nil
}

func (p *GitHubProvider) GetPRState(workDir, branchName string) (string, error) {
	output, err := p.run(workDir, "get PR state",
		"pr", "view", branchName, "--json", "state", "--jq", ".state")
	if err != nil {
		return "", fmt.Errorf("failed to get PR state: %w\nOutput: %s", err, string(output))
	}
	return strings.ToLower(strings.TrimSpace(string(output))), nil
}

func (p *GitHubProvider) GetPRStateByID(workDir, prID string) (string, error) {
	output, err := p.run(workDir, "get PR state by ID",
		"pr", "view", prID, "--json", "state", "--jq", ".state")
	if err != nil {
		return "", fmt.Errorf("failed to get PR state by ID: %w\nOutput: %s", err, string(output))
	}
	return strings.ToLower(strings.TrimSpace(string(output))), nil
}

// ExtractPRIDFromURL returns the trailing number of a PR URL
// (https://github.com/owner/repo/pull/1234 → "1234").
func (p *GitHubProvider) ExtractPRIDFromURL(prURL string) string {
	return lastPathSegment(prURL)
}

// CommitURL builds a GitHub commit URL: <repoURL>/commit/<sha>.
func (p *GitHubProvider) CommitURL(repoURL, sha string) string {
	return fmt.Sprintf("%s/commit/%s", strings.TrimRight(repoURL, "/"), sha)
}

// TemplateSearchPaths lists the standard GitHub PR template locations.
func (p *GitHubProvider) TemplateSearchPaths() []string {
	return []string{
		"pull_request_template.md",
		"PULL_REQUEST_TEMPLATE.md",
		".github/pull_request_template.md",
		".github/PULL_REQUEST_TEMPLATE.md",
		"docs/pull_request_template.md",
		"docs/PULL_REQUEST_TEMPLATE.md",
		".github/PULL_REQUEST_TEMPLATE/pull_request_template.md",
	}
}

func (p *GitHubProvider) ParseRemoteURL(remoteURL string) (*RepoDetails, error) {
	host, segs, err := parseHostAndSegments(remoteURL)
	if err != nil {
		return nil, err
	}
	if len(segs) != 2 {
		return nil, fmt.Errorf("invalid GitHub remote URL (expected owner/repo): %s", remoteURL)
	}
	return &RepoDetails{Host: host, Owner: segs[0], Repo: segs[1]}, nil
}

// BuildAuthenticatedHTTPSURL builds the token-injected HTTPS remote.
// GitHub uses the magic username "x-access-token".
func (p *GitHubProvider) BuildAuthenticatedHTTPSURL(d *RepoDetails, token string) string {
	return fmt.Sprintf("https://x-access-token:%s@%s/%s/%s.git", token, d.Host, d.Owner, d.Repo)
}

// lastPathSegment returns the final "/"-delimited segment of a URL. It works for
// both GitHub (/pull/123) and GitLab (/-/merge_requests/123). Behavior is
// identical to nairid's historical extractor: a trailing slash yields "".
func lastPathSegment(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parts := strings.Split(rawURL, "/")
	if len(parts) > 0 && parts[len(parts)-1] != "" {
		return parts[len(parts)-1]
	}
	return ""
}
