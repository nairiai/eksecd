package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"nairid/core/log"
)

// GitLabProvider drives merge requests through the GitLab CLI (`glab`).
//
// nairid configures `glab` non-interactively by exporting GITLAB_HOST and
// GITLAB_TOKEN on every invocation (the user chose nairid-owns-the-subprocess-env
// over relying on ambient container env). When nairid has no token of its own it
// leaves any ambient GITLAB_TOKEN/GITLAB_HOST untouched.
type GitLabProvider struct {
	host    string // host[:port] — used only as a fallback identity
	baseURL string // scheme://host[:port] — exported as GITLAB_HOST for glab
	cls     classifier
}

// newGitLabProvider builds a GitLab provider for a given remote host. The host
// (and scheme) can be overridden by NAIRI_GIT_HOST, which is the canonical
// source of truth for self-hosted deployments (full URL form recommended).
func newGitLabProvider(remoteHost string) *GitLabProvider {
	host := remoteHost
	baseURL := ""
	if remoteHost != "" {
		baseURL = "https://" + remoteHost
	}
	if env := strings.TrimSpace(os.Getenv("NAIRI_GIT_HOST")); env != "" {
		baseURL = strings.TrimRight(env, "/")
		host = hostWithoutScheme(baseURL)
	}
	return &GitLabProvider{host: host, baseURL: baseURL, cls: gitLabClassifier()}
}

// gitLabClassifier holds the error patterns that mark a `glab` failure as a rate
// limit or otherwise transient/recoverable error. 401/Unauthorized is
// deliberately excluded: auth failures are not transient and retrying wastes the
// backoff window.
func gitLabClassifier() classifier {
	return classifier{
		rateLimitPatterns: []string{
			"rate limit",
			"too many requests",
			"429",
			"retry later",
		},
		recoverablePatterns: []string{
			"timeout",
			"i/o timeout",
			"connection timeout",
			"dial tcp",
			"context deadline exceeded",
			"read timed out",
			"503",
			"service unavailable",
			"502",
			"bad gateway",
			"504",
			"gateway timeout",
		},
	}
}

func (p *GitLabProvider) Name() string    { return "gitlab" }
func (p *GitLabProvider) CLIName() string { return "glab" }

// glabEnv returns the process environment plus the GitLab CLI configuration.
// GITLAB_HOST/GITLAB_TOKEN are only appended when nairid actually has a value,
// so an ambient container-provided value is preserved otherwise (later entries
// win in exec, so our values override when present).
func (p *GitLabProvider) glabEnv() []string {
	env := os.Environ()
	if p.baseURL != "" {
		env = append(env, "GITLAB_HOST="+p.baseURL)
	}
	if token := resolveGitToken(); token != "" {
		env = append(env, "GITLAB_TOKEN="+token)
	}
	if ca := strings.TrimSpace(os.Getenv("NAIRI_GIT_SSL_CA_FILE")); ca != "" {
		// glab is a Go HTTPS client and honors SSL_CERT_FILE for custom CAs.
		env = append(env, "SSL_CERT_FILE="+ca)
	}
	return env
}

func (p *GitLabProvider) run(workDir, operationName string, args ...string) ([]byte, error) {
	cmd := exec.Command("glab", args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = p.glabEnv()
	return executeWithRetry(cmd, workDir, operationName, "GitLab", p.cls)
}

func (p *GitLabProvider) IsCLIAvailable(workDir string) error {
	log.Info("📋 Starting to check GitLab CLI availability")

	cmd := exec.Command("glab", "--version")
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = p.glabEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Error("❌ GitLab CLI not found: %v\nOutput: %s", err, string(out))
		return fmt.Errorf("gitlab cli (glab) not found: %w\nOutput: %s", err, string(out))
	}
	if major, minor, ok := parseGlabVersion(string(out)); ok && !versionAtLeast(major, minor, 1, 40) {
		return fmt.Errorf("glab %d.%d is too old; nairid requires glab >= 1.40", major, minor)
	}

	// Auth + host probe. `glab auth status` can lie when env vars are set wrong,
	// so hit the API directly. Use a generous timeout for cold self-hosted
	// instances (Sidekiq workers can be slow on the first request after restart).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, "glab", "api", "user")
	if workDir != "" {
		probe.Dir = workDir
	}
	probe.Env = p.glabEnv()
	if out, err := probe.CombinedOutput(); err != nil {
		log.Error("❌ GitLab CLI not authenticated: %v\nOutput: %s", err, string(out))
		return fmt.Errorf("glab not authenticated for host %q (set NAIRI_GIT_TOKEN/GITLAB_TOKEN and NAIRI_GIT_HOST): %w\nOutput: %s", p.baseURL, err, string(out))
	}

	log.Info("✅ GitLab CLI is available and authenticated")
	return nil
}

// glabMR is the subset of `glab mr view -F json` output that nairid needs. These
// four fields have been stable across glab releases.
type glabMR struct {
	WebURL      string `json:"web_url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	State       string `json:"state"`
	IID         int    `json:"iid"`
}

func (p *GitLabProvider) viewMR(workDir, ref string) (*glabMR, error) {
	output, err := p.run(workDir, "view merge request", "mr", "view", ref, "-F", "json")
	if err != nil {
		return nil, fmt.Errorf("failed to view merge request %q: %w\nOutput: %s", ref, err, string(output))
	}
	var mr glabMR
	if err := json.Unmarshal(output, &mr); err != nil {
		return nil, fmt.Errorf("failed to parse glab mr view JSON for %q: %w\nOutput: %s", ref, err, string(output))
	}
	return &mr, nil
}

func (p *GitLabProvider) CreatePR(workDir, title, body, baseBranch string) (string, error) {
	args := []string{"mr", "create", "--title", title, "--description", body, "--target-branch", baseBranch, "--yes"}
	if src, err := currentBranch(workDir); err == nil && src != "" {
		args = append(args, "--source-branch", src)
	}

	output, err := p.run(workDir, "create merge request", args...)
	if err != nil {
		log.Error("❌ GitLab MR creation failed for title '%s': %v\nOutput: %s", title, err, string(output))
		return "", fmt.Errorf("gitlab mr creation failed: %w\nOutput: %s", err, string(output))
	}

	if url := extractMRURL(string(output)); url != "" {
		return url, nil
	}
	// glab's create output format isn't guaranteed; fall back to querying the MR
	// for the current branch.
	if src, err := currentBranch(workDir); err == nil && src != "" {
		if url, err := p.GetPRURL(workDir, src); err == nil && url != "" {
			return url, nil
		}
	}
	return "", fmt.Errorf("created merge request but could not determine its URL\nOutput: %s", strings.TrimSpace(string(output)))
}

func (p *GitLabProvider) GetPRURL(workDir, branchName string) (string, error) {
	mr, err := p.viewMR(workDir, branchName)
	if err != nil {
		return "", err
	}
	return mr.WebURL, nil
}

func (p *GitLabProvider) HasExistingPR(workDir, branchName string) (bool, error) {
	output, err := p.run(workDir, "check existing MR", "mr", "list", "--source-branch", branchName, "-F", "json")
	if err != nil {
		return false, fmt.Errorf("failed to check for existing merge request: %w\nOutput: %s", err, string(output))
	}
	var mrs []glabMR
	if err := json.Unmarshal(output, &mrs); err != nil {
		return false, fmt.Errorf("failed to parse glab mr list JSON: %w\nOutput: %s", err, string(output))
	}
	return len(mrs) > 0, nil
}

func (p *GitLabProvider) GetPRTitle(workDir, branchName string) (string, error) {
	mr, err := p.viewMR(workDir, branchName)
	if err != nil {
		return "", err
	}
	return mr.Title, nil
}

func (p *GitLabProvider) GetPRDescription(workDir, branchName string) (string, error) {
	mr, err := p.viewMR(workDir, branchName)
	if err != nil {
		return "", err
	}
	return mr.Description, nil
}

func (p *GitLabProvider) UpdatePRTitle(workDir, branchName, newTitle string) error {
	output, err := p.run(workDir, "update MR title", "mr", "update", branchName, "--title", newTitle)
	if err != nil {
		return fmt.Errorf("failed to update merge request title: %w\nOutput: %s", err, string(output))
	}
	return nil
}

func (p *GitLabProvider) UpdatePRDescription(workDir, branchName, newBody string) error {
	output, err := p.run(workDir, "update MR description", "mr", "update", branchName, "--description", newBody)
	if err != nil {
		return fmt.Errorf("failed to update merge request description: %w\nOutput: %s", err, string(output))
	}
	return nil
}

func (p *GitLabProvider) GetPRState(workDir, branchName string) (string, error) {
	mr, err := p.viewMR(workDir, branchName)
	if err != nil {
		return "", err
	}
	return normalizeGitLabState(mr.State), nil
}

func (p *GitLabProvider) GetPRStateByID(workDir, prID string) (string, error) {
	mr, err := p.viewMR(workDir, prID)
	if err != nil {
		return "", err
	}
	return normalizeGitLabState(mr.State), nil
}

// ExtractPRIDFromURL returns the trailing IID of an MR URL
// (https://gitlab.com/group/repo/-/merge_requests/123 → "123").
func (p *GitLabProvider) ExtractPRIDFromURL(prURL string) string {
	return lastPathSegment(prURL)
}

// CommitURL builds a GitLab commit URL: <repoURL>/-/commit/<sha>. GitLab routes
// path-based actions under "/-/".
func (p *GitLabProvider) CommitURL(repoURL, sha string) string {
	return fmt.Sprintf("%s/-/commit/%s", strings.TrimRight(repoURL, "/"), sha)
}

// TemplateSearchPaths lists GitLab merge-request template locations. v1 only
// looks for the project default templates; named templates and the server-side
// project default are out of scope.
func (p *GitLabProvider) TemplateSearchPaths() []string {
	return []string{
		".gitlab/merge_request_templates/Default.md",
		".gitlab/merge_request_templates/default.md",
	}
}

func (p *GitLabProvider) ParseRemoteURL(remoteURL string) (*RepoDetails, error) {
	host, segs, err := parseHostAndSegments(remoteURL)
	if err != nil {
		return nil, err
	}
	if len(segs) < 2 {
		return nil, fmt.Errorf("invalid GitLab remote URL (expected group/repo): %s", remoteURL)
	}
	return &RepoDetails{
		Host:  host,
		Owner: strings.Join(segs[:len(segs)-1], "/"), // full subgroup path
		Repo:  segs[len(segs)-1],
	}, nil
}

// BuildAuthenticatedHTTPSURL builds the token-injected HTTPS remote. GitLab uses
// the magic username "oauth2", which works for PAT, Project, and Group Access
// Tokens interchangeably.
func (p *GitLabProvider) BuildAuthenticatedHTTPSURL(d *RepoDetails, token string) string {
	return fmt.Sprintf("https://oauth2:%s@%s/%s/%s.git", token, d.Host, d.Owner, d.Repo)
}

// normalizeGitLabState maps GitLab's MR states onto nairid's open/closed/merged
// vocabulary so usecase-layer consumers don't need to know the difference.
func normalizeGitLabState(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "opened":
		return "open"
	case "merged":
		return "merged"
	case "closed", "locked":
		return "closed"
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

// resolveGitToken returns the token nairid should use, preferring the
// forge-neutral NAIRI_GIT_TOKEN and falling back to GH_TOKEN for compatibility.
func resolveGitToken() string {
	if t := strings.TrimSpace(os.Getenv("NAIRI_GIT_TOKEN")); t != "" {
		return t
	}
	return strings.TrimSpace(os.Getenv("GH_TOKEN"))
}

func hostWithoutScheme(u string) string {
	if i := strings.Index(u, "://"); i != -1 {
		return u[i+3:]
	}
	return u
}

func currentBranch(workDir string) (string, error) {
	cmd := exec.Command("git", "branch", "--show-current")
	if workDir != "" {
		cmd.Dir = workDir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

var (
	glabVersionRe = regexp.MustCompile(`(\d+)\.(\d+)(?:\.\d+)?`)
	mrURLRe       = regexp.MustCompile(`https?://\S*/-/merge_requests/\d+`)
)

func parseGlabVersion(s string) (major, minor int, ok bool) {
	m := glabVersionRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return major, minor, true
}

func versionAtLeast(major, minor, wantMajor, wantMinor int) bool {
	if major != wantMajor {
		return major > wantMajor
	}
	return minor >= wantMinor
}

func extractMRURL(output string) string {
	return mrURLRe.FindString(output)
}
