package opencode

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"nairid/clients"
	"nairid/core"
	"nairid/core/log"
)

type OpenCodeClient struct {
	// No permissionsMode needed as we only support `bypassPermissions` for now.
}

func NewOpenCodeClient() *OpenCodeClient {
	return &OpenCodeClient{}
}

func (c *OpenCodeClient) StartNewSession(prompt string, options *clients.OpenCodeOptions, onLine clients.ProgressCallback) (string, error) {
	log.Info("📋 Starting to create new OpenCode session")

	args := []string{
		"run",
		"--format", "json",
		"--agent", "build", // Always use build mode until `acceptEdits` support is added
	}

	// Add model from options if provided
	if options != nil && options.Model != "" {
		args = append(args, "--model", options.Model)
	}

	// Append prompt as the last argument
	args = append(args, prompt)

	log.Info("Starting new OpenCode session with prompt: %s", prompt)
	log.Info("Command arguments: %v", args)

	timeout := clients.SessionTimeout()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := buildCommand(ctx, options, args)

	log.Info("Running OpenCode command (timeout: %s)", timeout)
	result, err := clients.RunCommandStreaming(ctx, cmd, onLine)
	if err != nil {
		return "", handleCommandError(ctx, err, "OpenCode", timeout)
	}

	log.Info("OpenCode command completed successfully, outputLength: %d", len(result))
	log.Info("📋 Completed successfully - created new OpenCode session")
	return result, nil
}

func (c *OpenCodeClient) ContinueSession(sessionID, prompt string, options *clients.OpenCodeOptions, onLine clients.ProgressCallback) (string, error) {
	log.Info("📋 Starting to continue OpenCode session: %s", sessionID)

	args := []string{
		"run",
		"--session", sessionID,
		"--format", "json",
		"--agent", "build", // Always use build mode until `acceptEdits` support is added
	}

	// Add model from options if provided
	if options != nil && options.Model != "" {
		args = append(args, "--model", options.Model)
	}

	// Append prompt as the last argument
	args = append(args, prompt)

	log.Info("Executing OpenCode command with sessionID: %s, prompt: %s", sessionID, prompt)
	log.Info("Command arguments: %v", args)

	timeout := clients.SessionTimeout()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := buildCommand(ctx, options, args)

	log.Info("Running OpenCode command (timeout: %s)", timeout)
	result, err := clients.RunCommandStreaming(ctx, cmd, onLine)
	if err != nil {
		return "", handleCommandError(ctx, err, "OpenCode", timeout)
	}

	log.Info("OpenCode command completed successfully, outputLength: %d", len(result))
	log.Info("📋 Completed successfully - continued OpenCode session")
	return result, nil
}

// handleCommandError converts a clients.CommandError to a core.ErrClaudeCommandErr,
// including timeout detection via context deadline.
func handleCommandError(ctx context.Context, err error, agentName string, timeout time.Duration) error {
	var cmdErr *clients.CommandError
	if errors.As(err, &cmdErr) {
		if ctx.Err() == context.DeadlineExceeded {
			log.Error("⏰ %s session timed out after %s", agentName, timeout)
			return &core.ErrClaudeCommandErr{
				Err:    fmt.Errorf("session timed out after %s: %w", timeout, cmdErr.Err),
				Output: cmdErr.Output,
			}
		}
		return &core.ErrClaudeCommandErr{
			Err:    cmdErr.Err,
			Output: cmdErr.Output,
		}
	}
	return err
}

// buildCommand creates the appropriate exec.Cmd with context based on options
func buildCommand(ctx context.Context, options *clients.OpenCodeOptions, args []string) *exec.Cmd {
	if options != nil && options.WorkDir != "" {
		log.Info("Using working directory: %s", options.WorkDir)
		return clients.BuildAgentCommandWithContextAndWorkDir(ctx, options.WorkDir, "opencode", args...)
	}
	return clients.BuildAgentCommandWithContext(ctx, "opencode", args...)
}
