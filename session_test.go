package claudecode_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

// setupSessionTestProject creates a temp config dir with a project directory
// and sets CLAUDE_CONFIG_DIR for testing.
func setupSessionTestProject(t *testing.T) string {
	t.Helper()
	cfgDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)

	projDir := filepath.Join(cfgDir, "projects", "-test-project")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("creating project dir: %v", err)
	}
	return projDir
}

// writeTestSession writes a session JSONL file with the given entries.
func writeTestSession(t *testing.T, dir, sessionID string, entries []map[string]any) {
	t.Helper()
	path := filepath.Join(dir, sessionID+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating session file: %v", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("closing session file: %v", err)
		}
	}()

	enc := json.NewEncoder(f)
	for _, entry := range entries {
		if err := enc.Encode(entry); err != nil {
			t.Fatalf("writing entry: %v", err)
		}
	}
}

func TestPublicListSessions(t *testing.T) {
	projDir := setupSessionTestProject(t)

	writeTestSession(t, projDir, "sess-001", []map[string]any{
		{"type": "queue-operation", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "sess-001"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "First session"}, "uuid": "u1", "timestamp": "2026-01-01T00:00:01Z", "sessionId": "sess-001"},
	})
	writeTestSession(t, projDir, "sess-002", []map[string]any{
		{"type": "queue-operation", "timestamp": "2026-02-01T00:00:00Z", "sessionId": "sess-002"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "Second session"}, "uuid": "u2", "timestamp": "2026-02-01T00:00:01Z", "sessionId": "sess-002"},
	})

	sessions, err := claudecode.ListSessions(claudecode.WithSessionDirectory("/test/project"))
	if err != nil {
		t.Fatalf("ListSessions() error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
	// Verify type is accessible as SDKSessionInfo
	var _ string = sessions[0].Summary
	var _ string = sessions[0].SessionID
}

func TestPublicGetSessionMessages(t *testing.T) {
	projDir := setupSessionTestProject(t)

	writeTestSession(t, projDir, "sess-msg", []map[string]any{
		{"type": "user", "message": map[string]any{"role": "user", "content": "Hello"}, "uuid": "u1", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "sess-msg"},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "Hi!"}}}, "uuid": "a1", "timestamp": "2026-01-01T00:00:01Z", "sessionId": "sess-msg"},
	})

	msgs, err := claudecode.GetSessionMessages("sess-msg", claudecode.WithSessionDirectory("/test/project"))
	if err != nil {
		t.Fatalf("GetSessionMessages() error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	// Verify type is accessible as SessionMessage
	var _ string = msgs[0].Type
	var _ string = msgs[0].UUID
}

func TestPublicGetSessionInfo(t *testing.T) {
	projDir := setupSessionTestProject(t)

	writeTestSession(t, projDir, "sess-info", []map[string]any{
		{"type": "queue-operation", "timestamp": "2026-03-01T00:00:00Z", "sessionId": "sess-info"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "Test prompt"}, "uuid": "u1", "timestamp": "2026-03-01T00:00:01Z", "cwd": "/my/project", "gitBranch": "main", "sessionId": "sess-info"},
	})

	info, err := claudecode.GetSessionInfo("sess-info", claudecode.WithSessionDirectory("/test/project"))
	if err != nil {
		t.Fatalf("GetSessionInfo() error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	// Summary falls back to timestamp when no custom title is set.
	want := "New session - 2026-03-01T00:00:00Z"
	if info.Summary != want {
		t.Errorf("Summary = %q, want %q", info.Summary, want)
	}

	// Not found returns nil
	info, err = claudecode.GetSessionInfo("nonexistent", claudecode.WithSessionDirectory("/test/project"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Error("expected nil for nonexistent session")
	}
}
