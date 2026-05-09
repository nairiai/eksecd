package usecases

import (
	"os"
	"path/filepath"
	"testing"

	"nairid/clients"
	"nairid/models"
)

// TestCleanupOrphanedWorktrees_FallbackOnGitFailure verifies that when
// `git worktree remove` fails (for example because the bare repo's
// .git/worktrees/ directory has been wiped), CleanupOrphanedWorktrees falls
// back to os.RemoveAll so the orphan directory is still removed.
//
// Regression: prior to the fix, `CleanupOrphanedWorktrees` only logged a
// warning when git refused to remove the worktree and left the directory on
// disk forever. On a host whose bare repo lost its worktrees registry, this
// caused tens of GB of orphaned job worktree directories to accumulate over
// time because the periodic cleanup hit the same git failure on every tick.
func TestCleanupOrphanedWorktrees_FallbackOnGitFailure(t *testing.T) {
	mainRepo, worktreeBase, cleanup := setupTestGitRepoWithRemote(t)
	defer cleanup()

	gitClient := clients.NewGitClient()
	gitClient.SetRepoPathProvider(func() string { return mainRepo })

	appState := models.NewAppState("test-agent", filepath.Join(worktreeBase, "state.json"))
	appState.SetRepositoryContext(&models.RepositoryContext{
		RepoPath:   mainRepo,
		IsRepoMode: true,
	})

	gitUseCase := NewGitUseCase(gitClient, nil, appState)
	// GetWorktreeBasePath honors AGENT_EXEC_USER and falls back to $HOME, so
	// override $HOME for the duration of the test to point at our temp dir.
	t.Setenv("AGENT_EXEC_USER", "")
	t.Setenv("HOME", worktreeBase)

	// The function expects ~/.eksec_worktrees as the base path; create that
	// layout under our temp HOME and place an orphan directory inside it.
	resolvedBase, err := gitUseCase.GetWorktreeBasePath()
	if err != nil {
		t.Fatalf("Failed to get worktree base path: %v", err)
	}
	if err := os.MkdirAll(resolvedBase, 0755); err != nil {
		t.Fatalf("Failed to create worktree base path: %v", err)
	}

	// Create a directory that LOOKS like a worktree but is not registered with
	// git — simulating the post-wipe state where .git/worktrees/ has no entry
	// for it. `git worktree remove` will fail on this path.
	orphanPath := filepath.Join(resolvedBase, "job_01KQORPHANEDWORKTREEXYZ")
	if err := os.MkdirAll(orphanPath, 0755); err != nil {
		t.Fatalf("Failed to create orphan directory: %v", err)
	}
	// Drop a file inside so we can also verify recursive removal.
	if err := os.WriteFile(filepath.Join(orphanPath, "marker"), []byte("orphan"), 0644); err != nil {
		t.Fatalf("Failed to write marker file: %v", err)
	}

	if err := gitUseCase.CleanupOrphanedWorktrees(); err != nil {
		t.Fatalf("CleanupOrphanedWorktrees failed: %v", err)
	}

	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Errorf("Orphan worktree directory was not removed via fallback: %v", err)
	}
}

// TestCleanupOrphanedWorktrees_SkipsTrackedWorktrees verifies that worktrees
// referenced by a tracked job are never touched, even if `git worktree remove`
// would also fail on them.
func TestCleanupOrphanedWorktrees_SkipsTrackedWorktrees(t *testing.T) {
	mainRepo, worktreeBase, cleanup := setupTestGitRepoWithRemote(t)
	defer cleanup()

	gitClient := clients.NewGitClient()
	gitClient.SetRepoPathProvider(func() string { return mainRepo })

	appState := models.NewAppState("test-agent", filepath.Join(worktreeBase, "state.json"))
	appState.SetRepositoryContext(&models.RepositoryContext{
		RepoPath:   mainRepo,
		IsRepoMode: true,
	})

	gitUseCase := NewGitUseCase(gitClient, nil, appState)
	t.Setenv("AGENT_EXEC_USER", "")
	t.Setenv("HOME", worktreeBase)

	resolvedBase, err := gitUseCase.GetWorktreeBasePath()
	if err != nil {
		t.Fatalf("Failed to get worktree base path: %v", err)
	}
	if err := os.MkdirAll(resolvedBase, 0755); err != nil {
		t.Fatalf("Failed to create worktree base path: %v", err)
	}

	trackedPath := filepath.Join(resolvedBase, "job_01KQTRACKEDXYZ123456789")
	if err := os.MkdirAll(trackedPath, 0755); err != nil {
		t.Fatalf("Failed to create tracked directory: %v", err)
	}

	// Register the path with appState so CleanupOrphanedWorktrees treats it
	// as live.
	if err := appState.UpdateJobData("job_01KQTRACKEDXYZ123456789", models.JobData{
		JobID:        "job_01KQTRACKEDXYZ123456789",
		BranchName:   "nairid/test-branch",
		WorktreePath: trackedPath,
		Status:       models.JobStatusInProgress,
	}); err != nil {
		t.Fatalf("Failed to register tracked job: %v", err)
	}

	if err := gitUseCase.CleanupOrphanedWorktrees(); err != nil {
		t.Fatalf("CleanupOrphanedWorktrees failed: %v", err)
	}

	if _, err := os.Stat(trackedPath); os.IsNotExist(err) {
		t.Error("Tracked worktree was incorrectly removed")
	}
}
