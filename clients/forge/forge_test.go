package forge

import "testing"

func TestParseHostAndSegments(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantHost string
		wantSegs []string
		wantErr  bool
	}{
		{"github https", "https://github.com/owner/repo.git", "github.com", []string{"owner", "repo"}, false},
		{"github https no .git", "https://github.com/owner/repo", "github.com", []string{"owner", "repo"}, false},
		{"github scp ssh", "git@github.com:owner/repo.git", "github.com", []string{"owner", "repo"}, false},
		{"github token-injected", "https://x-access-token:ghs_abc@github.com/owner/repo.git", "github.com", []string{"owner", "repo"}, false},
		{"gitlab https subgroups", "https://gitlab.example.com/acme/backend/services/api.git", "gitlab.example.com", []string{"acme", "backend", "services", "api"}, false},
		{"gitlab scp subgroups", "git@gitlab.example.com:acme/backend/api.git", "gitlab.example.com", []string{"acme", "backend", "api"}, false},
		{"gitlab oauth2-injected", "https://oauth2:glpat_xx@gitlab.example.com/group/repo.git", "gitlab.example.com", []string{"group", "repo"}, false},
		{"https custom port", "https://git.acme.com:8443/group/repo.git", "git.acme.com:8443", []string{"group", "repo"}, false},
		{"ssh:// with port", "ssh://git@git.acme.com:2222/group/repo.git", "git.acme.com:2222", []string{"group", "repo"}, false},
		{"empty", "", "", nil, true},
		{"garbage", "not-a-url", "", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, segs, err := parseHostAndSegments(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got host=%q segs=%v", tt.url, host, segs)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.url, err)
			}
			if host != tt.wantHost {
				t.Errorf("host = %q, want %q", host, tt.wantHost)
			}
			if !equalStrings(segs, tt.wantSegs) {
				t.Errorf("segs = %v, want %v", segs, tt.wantSegs)
			}
		})
	}
}

func TestGitHubParseRemoteURL(t *testing.T) {
	p := NewGitHubProvider()
	d, err := p.ParseRemoteURL("https://github.com/owner/repo.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Host != "github.com" || d.Owner != "owner" || d.Repo != "repo" {
		t.Errorf("got %+v", d)
	}
	// GitHub requires exactly owner/repo.
	if _, err := p.ParseRemoteURL("https://github.com/a/b/c.git"); err == nil {
		t.Error("expected error for >2 path segments on GitHub")
	}
}

func TestGitLabParseRemoteURL_Subgroups(t *testing.T) {
	p := newGitLabProvider("gitlab.example.com")
	d, err := p.ParseRemoteURL("https://gitlab.example.com/acme/backend/services/api.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Owner != "acme/backend/services" {
		t.Errorf("Owner = %q, want full subgroup path", d.Owner)
	}
	if d.Repo != "api" {
		t.Errorf("Repo = %q, want api", d.Repo)
	}
}

func TestBuildAuthenticatedHTTPSURL_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		p    Provider
		url  string
	}{
		{"github", NewGitHubProvider(), "https://github.com/owner/repo.git"},
		{"gitlab", newGitLabProvider("gitlab.example.com"), "https://gitlab.example.com/acme/backend/api.git"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := c.p.ParseRemoteURL(c.url)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			auth := c.p.BuildAuthenticatedHTTPSURL(d, "TOK")
			d2, err := c.p.ParseRemoteURL(auth)
			if err != nil {
				t.Fatalf("re-parse %q: %v", auth, err)
			}
			if *d != *d2 {
				t.Errorf("round-trip mismatch: %+v != %+v (auth=%q)", d, d2, auth)
			}
		})
	}
}

func TestBuildAuthenticatedHTTPSURL_Schemes(t *testing.T) {
	gh := NewGitHubProvider()
	d, _ := gh.ParseRemoteURL("https://github.com/owner/repo.git")
	if got := gh.BuildAuthenticatedHTTPSURL(d, "TOK"); got != "https://x-access-token:TOK@github.com/owner/repo.git" {
		t.Errorf("github auth url = %q", got)
	}
	gl := newGitLabProvider("gitlab.example.com")
	d2, _ := gl.ParseRemoteURL("https://gitlab.example.com/group/sub/repo.git")
	if got := gl.BuildAuthenticatedHTTPSURL(d2, "TOK"); got != "https://oauth2:TOK@gitlab.example.com/group/sub/repo.git" {
		t.Errorf("gitlab auth url = %q", got)
	}
}

func TestExtractPRIDFromURL(t *testing.T) {
	gh := NewGitHubProvider()
	gl := newGitLabProvider("gitlab.example.com")
	tests := []struct {
		name, url, want string
		p               Provider
	}{
		{"github pull", "https://github.com/owner/repo/pull/123", "123", gh},
		{"gitlab mr", "https://gitlab.example.com/group/repo/-/merge_requests/456", "456", gl},
		{"github via gitlab provider", "https://github.com/owner/repo/pull/123", "123", gl},
		{"gitlab via github provider", "https://gitlab.example.com/g/r/-/merge_requests/7", "7", gh},
		{"empty", "", "", gh},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.ExtractPRIDFromURL(tt.url); got != tt.want {
				t.Errorf("ExtractPRIDFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestCommitURL(t *testing.T) {
	if got := NewGitHubProvider().CommitURL("https://github.com/owner/repo", "abc123"); got != "https://github.com/owner/repo/commit/abc123" {
		t.Errorf("github commit url = %q", got)
	}
	if got := newGitLabProvider("gitlab.example.com").CommitURL("https://gitlab.example.com/group/repo", "abc123"); got != "https://gitlab.example.com/group/repo/-/commit/abc123" {
		t.Errorf("gitlab commit url = %q", got)
	}
}

func TestNormalizeGitLabState(t *testing.T) {
	tests := map[string]string{
		"opened": "open",
		"closed": "closed",
		"merged": "merged",
		"locked": "closed",
		"OPENED": "open",
	}
	for in, want := range tests {
		if got := normalizeGitLabState(in); got != want {
			t.Errorf("normalizeGitLabState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetect(t *testing.T) {
	t.Run("github.com auto", func(t *testing.T) {
		t.Setenv("NAIRI_FORGE", "")
		p, err := Detect("https://github.com/owner/repo.git")
		if err != nil || p.Name() != "github" {
			t.Fatalf("got p=%v err=%v", p, err)
		}
	})
	t.Run("gitlab.com auto", func(t *testing.T) {
		t.Setenv("NAIRI_FORGE", "")
		t.Setenv("NAIRI_GIT_HOST", "")
		p, err := Detect("https://gitlab.com/group/sub/repo.git")
		if err != nil || p.Name() != "gitlab" {
			t.Fatalf("got p=%v err=%v", p, err)
		}
	})
	t.Run("unknown host defaults to github (GHES-compatible)", func(t *testing.T) {
		t.Setenv("NAIRI_FORGE", "")
		p, err := Detect("https://github.acme.com/owner/repo.git")
		if err != nil || p.Name() != "github" {
			t.Fatalf("expected github default for unknown host, got p=%v err=%v", p, err)
		}
	})
	t.Run("NAIRI_FORGE=gitlab forces gitlab", func(t *testing.T) {
		t.Setenv("NAIRI_FORGE", "gitlab")
		t.Setenv("NAIRI_GIT_HOST", "")
		p, err := Detect("https://code.acme.com/group/repo.git")
		if err != nil || p.Name() != "gitlab" {
			t.Fatalf("got p=%v err=%v", p, err)
		}
	})
	t.Run("NAIRI_FORGE=github forces github", func(t *testing.T) {
		t.Setenv("NAIRI_FORGE", "github")
		p, err := Detect("https://code.acme.com/group/repo.git")
		if err != nil || p.Name() != "github" {
			t.Fatalf("got p=%v err=%v", p, err)
		}
	})
	t.Run("invalid NAIRI_FORGE errors", func(t *testing.T) {
		t.Setenv("NAIRI_FORGE", "bitbucket")
		if _, err := Detect("https://github.com/owner/repo.git"); err == nil {
			t.Fatal("expected error for unknown NAIRI_FORGE value")
		}
	})
}

func TestGitLabHostFromEnv(t *testing.T) {
	t.Setenv("NAIRI_GIT_HOST", "https://code.acme.com")
	p := newGitLabProvider("ignored.example.com")
	if p.baseURL != "https://code.acme.com" {
		t.Errorf("baseURL = %q, want https://code.acme.com", p.baseURL)
	}
	if p.host != "code.acme.com" {
		t.Errorf("host = %q, want code.acme.com", p.host)
	}
}

func TestParseGlabVersion(t *testing.T) {
	tests := []struct {
		in           string
		major, minor int
		ok           bool
		atLeast140   bool
	}{
		{"glab 1.40.0", 1, 40, true, true},
		{"glab version 1.41.2 (2024-01-01)", 1, 41, true, true},
		{"glab 1.39.9", 1, 39, true, false},
		{"glab 2.0.0", 2, 0, true, true},
		{"no version here", 0, 0, false, false},
	}
	for _, tt := range tests {
		major, minor, ok := parseGlabVersion(tt.in)
		if ok != tt.ok || major != tt.major || minor != tt.minor {
			t.Errorf("parseGlabVersion(%q) = (%d,%d,%v), want (%d,%d,%v)", tt.in, major, minor, ok, tt.major, tt.minor, tt.ok)
		}
		if ok && versionAtLeast(major, minor, 1, 40) != tt.atLeast140 {
			t.Errorf("versionAtLeast(%d.%d, 1.40) = %v, want %v", major, minor, !tt.atLeast140, tt.atLeast140)
		}
	}
}

func TestExtractMRURL(t *testing.T) {
	out := "Creating merge request for foo into main\n\n!12 My title\n https://gitlab.example.com/group/repo/-/merge_requests/12\n"
	if got := extractMRURL(out); got != "https://gitlab.example.com/group/repo/-/merge_requests/12" {
		t.Errorf("extractMRURL = %q", got)
	}
	if got := extractMRURL("no url here"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
