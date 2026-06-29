// Package forge abstracts the forge-specific operations (merge/pull request
// management, authenticated remote URL construction, CLI availability, commit
// URL formatting, template locations) so that nairid can drive GitHub via `gh`
// and GitLab via `glab` through a single interface.
//
// Everything that is NOT forge-specific — plain `git` clone/fetch/push/branch/
// commit/worktree — stays in the clients package and is provider-neutral.
package forge

import (
	"fmt"
	"os"
	"strings"
)

// RepoDetails is the parsed identity of a remote repository.
//
// For GitLab, Owner is the full group path ("group/sub/sub2"), not just the
// top-level group, because GitLab supports arbitrary subgroup nesting.
type RepoDetails struct {
	Host  string // "github.com" or "gitlab.example.com" (may include ":port")
	Owner string // GitHub: "owner"; GitLab: full group path "group/sub"
	Repo  string // leaf repository name (no ".git")
}

// Provider is the forge-specific surface. Implementations: GitHubProvider (gh),
// GitLabProvider (glab). workDir may be empty, in which case the CLI runs in the
// process working directory; otherwise it is used as the command's directory so
// the same methods serve both the main repo and per-job worktrees.
type Provider interface {
	Name() string    // "github" | "gitlab"
	CLIName() string // "gh" | "glab"
	IsCLIAvailable(workDir string) error

	ParseRemoteURL(remoteURL string) (*RepoDetails, error)
	BuildAuthenticatedHTTPSURL(d *RepoDetails, token string) string

	CreatePR(workDir, title, body, baseBranch string) (string, error)
	GetPRURL(workDir, branchName string) (string, error)
	HasExistingPR(workDir, branchName string) (bool, error)
	GetPRTitle(workDir, branchName string) (string, error)
	GetPRDescription(workDir, branchName string) (string, error)
	UpdatePRTitle(workDir, branchName, newTitle string) error
	UpdatePRDescription(workDir, branchName, newBody string) error
	GetPRState(workDir, branchName string) (string, error) // normalized: open|closed|merged
	GetPRStateByID(workDir, prID string) (string, error)   // normalized: open|closed|merged

	ExtractPRIDFromURL(prURL string) string
	CommitURL(repoURL, sha string) string // GitHub: /commit/<sha>, GitLab: /-/commit/<sha>
	TemplateSearchPaths() []string        // PR/MR template candidate paths
}

// Detect selects the forge provider for a remote.
//
//   - NAIRI_FORGE=github|gitlab forces the provider (used for self-hosted hosts
//     whose domain doesn't contain "github"/"gitlab", e.g. code.acme.com).
//   - Otherwise the remote host is inspected: github.com (exact) → GitHub.
//   - Everything else fails loudly rather than guessing, because a substring
//     match on "gitlab" is unreliable for custom self-hosted domains.
func Detect(remoteURL string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("NAIRI_FORGE"))) {
	case "github":
		return NewGitHubProvider(), nil
	case "gitlab":
		host := ""
		if h, _, err := parseHostAndSegments(remoteURL); err == nil {
			host = h
		}
		return newGitLabProvider(host), nil
	case "":
		// fall through to auto-detection
	default:
		v := os.Getenv("NAIRI_FORGE")
		return nil, fmt.Errorf("unknown NAIRI_FORGE value %q (expected \"github\" or \"gitlab\")", v)
	}

	host, _, err := parseHostAndSegments(remoteURL)
	if err != nil {
		return nil, fmt.Errorf("could not parse remote URL %q to determine forge: %w; set NAIRI_FORGE=github|gitlab", remoteURL, err)
	}
	if hostWithoutPort(host) == "github.com" {
		return NewGitHubProvider(), nil
	}
	return nil, fmt.Errorf("could not determine git forge from remote host %q; set NAIRI_FORGE=github or NAIRI_FORGE=gitlab", host)
}

// parseHostAndSegments extracts the host (with optional :port) and the path
// segments of a remote URL. It accepts scp-style SSH (git@host:group/repo.git),
// ssh:// URLs (with optional port), and http(s):// URLs with optional userinfo
// (x-access-token:...@ or oauth2:...@). The ".git" suffix is optional.
func parseHostAndSegments(remoteURL string) (host string, segments []string, err error) {
	u := strings.TrimSpace(remoteURL)
	if u == "" {
		return "", nil, fmt.Errorf("empty remote URL")
	}

	var authorityAndPath string
	switch {
	case !strings.Contains(u, "://") && strings.Contains(u, "@") && strings.Contains(u, ":"):
		// scp-style: [user@]host:group/sub/repo.git
		at := strings.Index(u, "@")
		rest := u[at+1:]
		colon := strings.Index(rest, ":")
		if colon == -1 {
			return "", nil, fmt.Errorf("invalid SSH remote URL: %s", remoteURL)
		}
		authorityAndPath = rest[:colon] + "/" + strings.TrimPrefix(rest[colon+1:], "/")
	case strings.Contains(u, "://"):
		// scheme://[user[:pass]@]host[:port]/path
		rest := u[strings.Index(u, "://")+3:]
		slash := strings.Index(rest, "/")
		if slash == -1 {
			return "", nil, fmt.Errorf("remote URL has no repository path: %s", remoteURL)
		}
		authority := rest[:slash]
		if at := strings.LastIndex(authority, "@"); at != -1 {
			authority = authority[at+1:]
		}
		authorityAndPath = authority + rest[slash:]
	default:
		return "", nil, fmt.Errorf("unsupported remote URL format: %s", remoteURL)
	}

	authorityAndPath = strings.TrimSuffix(authorityAndPath, ".git")
	parts := strings.SplitN(authorityAndPath, "/", 2)
	if len(parts) != 2 || parts[0] == "" || strings.Trim(parts[1], "/") == "" {
		return "", nil, fmt.Errorf("remote URL missing repository path: %s", remoteURL)
	}

	host = parts[0]
	segs := strings.Split(strings.Trim(parts[1], "/"), "/")
	segs[len(segs)-1] = strings.TrimSuffix(segs[len(segs)-1], ".git")
	for _, s := range segs {
		if s == "" {
			return "", nil, fmt.Errorf("remote URL has an empty path segment: %s", remoteURL)
		}
	}
	return host, segs, nil
}

func hostWithoutPort(host string) string {
	if i := strings.LastIndex(host, ":"); i != -1 {
		return host[:i]
	}
	return host
}
