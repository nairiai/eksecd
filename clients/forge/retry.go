package forge

import (
	"os/exec"
	"strings"
	"time"

	"nairid/core/log"

	"github.com/cenkalti/backoff/v4"
)

// classifier decides whether a forge CLI error is a rate-limit error (longer
// backoff) or otherwise transient/recoverable (retry with normal backoff). The
// pattern sets differ per forge: GitHub and GitLab phrase rate limits and 5xx
// errors differently.
type classifier struct {
	rateLimitPatterns   []string
	recoverablePatterns []string
}

func matchesAny(patterns []string, errStr, outputStr string) bool {
	for _, p := range patterns {
		if strings.Contains(errStr, p) || strings.Contains(outputStr, p) {
			return true
		}
	}
	return false
}

func (c classifier) isRateLimit(err error, output string) bool {
	if err == nil {
		return false
	}
	return matchesAny(c.rateLimitPatterns, strings.ToLower(err.Error()), strings.ToLower(output))
}

func (c classifier) isRecoverable(err error, output string) bool {
	if err == nil {
		return false
	}
	if c.isRateLimit(err, output) {
		return true
	}
	return matchesAny(c.recoverablePatterns, strings.ToLower(err.Error()), strings.ToLower(output))
}

// executeWithRetry runs a forge CLI command with exponential backoff for
// recoverable errors. Rate-limit errors widen the backoff to 10 minutes since
// forge rate limits reset on longer windows.
//
// workDir may be empty (inherit the process working directory). It is applied
// only when cmd.Dir is not already set, so callers that pre-set cmd.Dir (e.g.
// to attach forge-specific env) keep their directory.
func executeWithRetry(cmd *exec.Cmd, workDir, operationName, forgeName string, cls classifier) ([]byte, error) {
	var output []byte
	var err error
	var rateLimitDetected bool

	retryBackoff := backoff.NewExponentialBackOff()
	retryBackoff.InitialInterval = 2 * time.Second
	retryBackoff.MaxInterval = 30 * time.Second
	retryBackoff.MaxElapsedTime = 2 * time.Minute
	retryBackoff.Multiplier = 2

	if cmd.Dir == "" {
		cmd.Dir = workDir
	}
	// Preserve working directory and environment for retries (a fresh exec.Cmd
	// is needed because a Cmd can only be run once).
	originalDir := cmd.Dir
	originalEnv := cmd.Env

	retryOperation := func() error {
		output, err = cmd.CombinedOutput()

		if err != nil && cls.isRecoverable(err, string(output)) {
			if !rateLimitDetected && cls.isRateLimit(err, string(output)) {
				rateLimitDetected = true
				retryBackoff.MaxInterval = 60 * time.Second
				retryBackoff.MaxElapsedTime = 10 * time.Minute
				log.Info("⏳ %s API rate limit detected for %s, extending retry window to 10 minutes...", forgeName, operationName)
			} else {
				log.Info("⏳ %s API recoverable error detected for %s, retrying...", forgeName, operationName)
			}
			cmd = exec.Command(cmd.Args[0], cmd.Args[1:]...)
			cmd.Dir = originalDir
			cmd.Env = originalEnv
			return err // triggers a retry
		}

		return nil // success or non-recoverable error: stop retrying
	}

	retryErr := backoff.Retry(retryOperation, retryBackoff)
	if retryErr != nil {
		if err != nil {
			return output, err
		}
		return output, retryErr
	}

	return output, err
}
