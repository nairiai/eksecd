package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"nairid/clients"
	"nairid/core/log"
	"nairid/services"
)

// dbQueryTimeout caps how long the post-turn `opencode db` lookup can take.
// The query is a simple indexed SELECT on a local SQLite file so a few
// seconds is generous.
const dbQueryTimeout = 10 * time.Second

// openCodeTokensRow mirrors the nested `tokens` JSON object returned by
// `opencode db --format json` when selecting message-level cost columns.
type openCodeTokensRow struct {
	Total     int64                  `json:"total"`
	Input     int64                  `json:"input"`
	Output    int64                  `json:"output"`
	Reasoning int64                  `json:"reasoning"`
	Cache     openCodeCacheTokensRow `json:"cache"`
}

type openCodeCacheTokensRow struct {
	Write int64 `json:"write"`
	Read  int64 `json:"read"`
}

// dbQueryFunc is the signature used to run `opencode db` queries.
// Production code uses runOpenCodeDB; tests can inject a stub.
type dbQueryFunc func(ctx context.Context, query string) ([]byte, error)

// buildOpenCodeDBCmd constructs the exec.Cmd that runOpenCodeDB will execute.
// Extracted so the cross-user routing can be asserted in a regression test
// (see cost_test.go) without actually shelling out.
//
// The lookup MUST run as the same user (and with the same HOME) as the
// `opencode run` subprocess that just finished, otherwise OpenCode resolves
// its SQLite path from the calling user's $HOME and reads an empty database.
// In managed-mode containers nairid runs as ccagent but the agent subprocess
// runs as agentrunner, so we go through clients.BuildAgentCommandWithContext
// which routes via `sudo -u agentrunner` with HOME=/home/agentrunner injected.
// In self-hosted mode this falls through to the current user, which is also
// the user that ran the OpenCode subprocess.
func buildOpenCodeDBCmd(ctx context.Context, query string) *exec.Cmd {
	return clients.BuildAgentCommandWithContext(ctx, "opencode", "db", "--format", "json", query)
}

// runOpenCodeDB shells out to `opencode db --format json "<query>"` and
// returns raw stdout bytes. Stderr is folded into the error on failure.
func runOpenCodeDB(ctx context.Context, query string) ([]byte, error) {
	cmd := buildOpenCodeDBCmd(ctx, query)
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return nil, fmt.Errorf("opencode db query failed: %w (stderr: %s)", err, stderr)
	}
	return out, nil
}

// fetchOpenCodeUsage queries the local OpenCode SQLite database for every
// assistant message in the given session that was created at or after
// runStart (i.e. the just-completed `opencode run` invocation), and folds
// them into a single CLIAgentUsage rollup.
//
// runStart is matched against OpenCode's millisecond epoch `time_created`
// column. Pass a slightly-pre-launch wall clock so the query is robust to
// minor clock skew between Go's time.Now() and OpenCode's internal stamps.
//
// Returns nil when no rows are found or any error occurs — the caller logs
// and proceeds with cost-less messaging, since this is best-effort.
func fetchOpenCodeUsage(
	ctx context.Context,
	sessionID string,
	runStart time.Time,
	query dbQueryFunc,
) *services.CLIAgentUsage {
	if sessionID == "" || sessionID == "unknown" {
		log.Warn("OpenCode cost lookup skipped: missing session id (got %q)", sessionID)
		return nil
	}

	// Embed sessionID and the unix-ms timestamp directly. Session IDs are
	// short ASCII tokens like "ses_1ac4e246..." and the timestamp is a
	// pure integer, so this is safe under any reasonable shell. We still
	// do a paranoia check to reject any session ID containing a single
	// quote, which would never appear in a real OpenCode id.
	if strings.ContainsAny(sessionID, "'\\\";\n") {
		log.Warn("OpenCode cost lookup skipped: suspicious session id rejected: %q", sessionID)
		return nil
	}
	runStartMs := runStart.UnixMilli()

	q := fmt.Sprintf(
		"SELECT "+
			"json_extract(data, '$.cost') AS cost, "+
			"json_extract(data, '$.tokens') AS tokens, "+
			"json_extract(data, '$.modelID') AS modelID "+
			"FROM message "+
			"WHERE session_id = '%s' "+
			"AND json_extract(data, '$.role') = 'assistant' "+
			"AND time_created >= %d "+
			"ORDER BY time_created ASC",
		sessionID,
		runStartMs,
	)

	cctx, cancel := context.WithTimeout(ctx, dbQueryTimeout)
	defer cancel()

	raw, err := query(cctx, q)
	if err != nil {
		log.Warn("OpenCode cost lookup: db query failed for session %s: %v", sessionID, err)
		return nil
	}

	// `opencode db` returns each row as a flat object with the SELECTed
	// columns as keys. json_extract on the `tokens` object returns the
	// nested JSON as a string, so we deserialise twice.
	var rawRows []struct {
		Cost   *float64 `json:"cost"`
		Tokens *string  `json:"tokens"`
		Model  *string  `json:"modelID"`
	}
	if err := json.Unmarshal(raw, &rawRows); err != nil {
		log.Warn("OpenCode cost lookup: failed to parse db output for session %s: %v", sessionID, err)
		return nil
	}
	if len(rawRows) == 0 {
		log.Warn("OpenCode cost lookup: no assistant rows found for session %s (cost will be NULL)", sessionID)
		return nil
	}

	var (
		totalCost   float64
		hasCost     bool
		inputSum    int64
		outputSum   int64
		cacheRead   int64
		cacheWrite  int64
		hasTokens   bool
		latestModel string
	)
	for _, r := range rawRows {
		if r.Cost != nil {
			totalCost += *r.Cost
			hasCost = true
		}
		if r.Tokens != nil && *r.Tokens != "" {
			var tk openCodeTokensRow
			if err := json.Unmarshal([]byte(*r.Tokens), &tk); err == nil {
				inputSum += tk.Input
				outputSum += tk.Output
				cacheRead += tk.Cache.Read
				cacheWrite += tk.Cache.Write
				hasTokens = true
			}
		}
		if r.Model != nil && *r.Model != "" {
			latestModel = *r.Model
		}
	}

	if !hasCost && !hasTokens {
		log.Warn("OpenCode cost lookup: rows returned but no cost or tokens parseable for session %s", sessionID)
		return nil
	}

	usage := &services.CLIAgentUsage{}
	if latestModel != "" {
		m := latestModel
		usage.Model = &m
	}
	if hasCost {
		c := totalCost
		usage.CostUSD = &c
	}
	if hasTokens {
		in := inputSum
		out := outputSum
		cr := cacheRead
		cw := cacheWrite
		usage.InputTokens = &in
		usage.OutputTokens = &out
		usage.CacheReadTokens = &cr
		usage.CacheWriteTokens = &cw
	}
	return usage
}
