package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEncodeCwd(t *testing.T) {
	tests := []struct {
		name string
		cwd  string
		want string
	}{
		{"unix path", "/Users/me/proj", "-Users-me-proj"},
		{"windows path", `C:\Users\me\proj`, "C--Users-me-proj"},
		{"spaces", "/path/to/my project", "-path-to-my-project"},
		{"dots", "/home/user/.config", "-home-user--config"},
		{"alphanumeric only", "abc123", "abc123"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodeCwd(tt.cwd)
			if got != tt.want {
				t.Errorf("encodeCwd(%q) = %q, want %q", tt.cwd, got, tt.want)
			}
		})
	}
}

func TestConfigDir(t *testing.T) {
	t.Run("respects CLAUDE_CONFIG_DIR", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", dir)

		got, err := configDir()
		if err != nil {
			t.Fatalf("configDir() error: %v", err)
		}
		if got != dir {
			t.Errorf("configDir() = %q, want %q", got, dir)
		}
	})

	t.Run("defaults to home/.claude", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", "")

		got, err := configDir()
		if err != nil {
			t.Fatalf("configDir() error: %v", err)
		}

		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".claude")
		if got != want {
			t.Errorf("configDir() = %q, want %q", got, want)
		}
	})
}

// setupTestProject creates a temp dir structured like ~/.claude/projects/<encoded>/
// and sets CLAUDE_CONFIG_DIR to point at it.
func setupTestProject(t *testing.T) (configDir string, projectDir string) {
	t.Helper()
	cfgDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)

	projDir := filepath.Join(cfgDir, "projects", "-test-project")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("creating project dir: %v", err)
	}
	return cfgDir, projDir
}

// writeSessionJSONL writes JSONL entries to a session file.
func writeSessionJSONL(t *testing.T, dir, sessionID string, entries []map[string]any) string {
	t.Helper()
	path := filepath.Join(dir, sessionID+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating session file: %v", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, entry := range entries {
		if err := enc.Encode(entry); err != nil {
			t.Fatalf("writing entry: %v", err)
		}
	}
	return path
}

func TestListSessions(t *testing.T) {
	_, projDir := setupTestProject(t)

	writeSessionJSONL(t, projDir, "aaaa-1111", []map[string]any{
		{"type": "queue-operation", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "aaaa-1111"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "Hello world"}, "uuid": "u1", "timestamp": "2026-01-01T00:00:01Z", "cwd": "/test/project", "gitBranch": "main", "sessionId": "aaaa-1111"},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "Hi!"}}}, "uuid": "a1", "timestamp": "2026-01-01T00:00:02Z", "sessionId": "aaaa-1111"},
	})

	writeSessionJSONL(t, projDir, "bbbb-2222", []map[string]any{
		{"type": "queue-operation", "timestamp": "2026-02-01T00:00:00Z", "sessionId": "bbbb-2222"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "Fix the bug"}, "uuid": "u2", "timestamp": "2026-02-01T00:00:01Z", "cwd": "/test/project", "gitBranch": "fix-branch", "sessionId": "bbbb-2222"},
		{"type": "custom-title", "customTitle": "Bug Fix Session", "sessionId": "bbbb-2222"},
	})

	t.Run("lists all sessions", func(t *testing.T) {
		sessions, err := ListSessions(WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("ListSessions() error: %v", err)
		}
		if len(sessions) != 2 {
			t.Fatalf("got %d sessions, want 2", len(sessions))
		}
	})

	t.Run("sorted by last modified descending", func(t *testing.T) {
		sessions, err := ListSessions(WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("ListSessions() error: %v", err)
		}
		if len(sessions) < 2 {
			t.Fatal("expected at least 2 sessions")
		}
		if sessions[0].LastModified < sessions[1].LastModified {
			t.Error("sessions not sorted by LastModified descending")
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		sessions, err := ListSessions(
			WithSessionDirectory("/test/project"),
			WithSessionLimit(1),
		)
		if err != nil {
			t.Fatalf("ListSessions() error: %v", err)
		}
		if len(sessions) != 1 {
			t.Fatalf("got %d sessions, want 1", len(sessions))
		}
	})

	t.Run("empty dir returns empty slice", func(t *testing.T) {
		sessions, err := ListSessions(WithSessionDirectory("/nonexistent/path"))
		if err != nil {
			t.Fatalf("ListSessions() error: %v", err)
		}
		if len(sessions) != 0 {
			t.Fatalf("got %d sessions, want 0", len(sessions))
		}
	})
}

func TestGetSessionMessages(t *testing.T) {
	_, projDir := setupTestProject(t)

	writeSessionJSONL(t, projDir, "cccc-3333", []map[string]any{
		{"type": "queue-operation", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "cccc-3333"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "Hello"}, "uuid": "u1", "timestamp": "2026-01-01T00:00:01Z", "sessionId": "cccc-3333"},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "Hi!"}}}, "uuid": "a1", "timestamp": "2026-01-01T00:00:02Z", "sessionId": "cccc-3333"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "Thanks"}, "uuid": "u2", "timestamp": "2026-01-01T00:00:03Z", "sessionId": "cccc-3333"},
		{"type": "last-prompt", "lastPrompt": "Thanks", "sessionId": "cccc-3333"},
	})

	t.Run("returns user and assistant messages", func(t *testing.T) {
		msgs, err := GetSessionMessages("cccc-3333", WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("GetSessionMessages() error: %v", err)
		}
		if len(msgs) != 3 {
			t.Fatalf("got %d messages, want 3", len(msgs))
		}
		if msgs[0].Type != "user" {
			t.Errorf("msgs[0].Type = %q, want %q", msgs[0].Type, "user")
		}
		if msgs[1].Type != "assistant" {
			t.Errorf("msgs[1].Type = %q, want %q", msgs[1].Type, "assistant")
		}
		if msgs[2].Type != "user" {
			t.Errorf("msgs[2].Type = %q, want %q", msgs[2].Type, "user")
		}
	})

	t.Run("preserves uuid and session_id", func(t *testing.T) {
		msgs, err := GetSessionMessages("cccc-3333", WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("GetSessionMessages() error: %v", err)
		}
		if msgs[0].UUID != "u1" {
			t.Errorf("msgs[0].UUID = %q, want %q", msgs[0].UUID, "u1")
		}
		if msgs[0].SessionID != "cccc-3333" {
			t.Errorf("msgs[0].SessionID = %q, want %q", msgs[0].SessionID, "cccc-3333")
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		msgs, err := GetSessionMessages("cccc-3333",
			WithSessionDirectory("/test/project"),
			WithSessionLimit(2),
		)
		if err != nil {
			t.Fatalf("GetSessionMessages() error: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("got %d messages, want 2", len(msgs))
		}
	})

	t.Run("respects offset", func(t *testing.T) {
		msgs, err := GetSessionMessages("cccc-3333",
			WithSessionDirectory("/test/project"),
			WithSessionOffset(1),
		)
		if err != nil {
			t.Fatalf("GetSessionMessages() error: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("got %d messages, want 2", len(msgs))
		}
		if msgs[0].Type != "assistant" {
			t.Errorf("msgs[0].Type = %q, want %q (after offset=1)", msgs[0].Type, "assistant")
		}
	})

	t.Run("offset and limit combined", func(t *testing.T) {
		msgs, err := GetSessionMessages("cccc-3333",
			WithSessionDirectory("/test/project"),
			WithSessionOffset(1),
			WithSessionLimit(1),
		)
		if err != nil {
			t.Fatalf("GetSessionMessages() error: %v", err)
		}
		if len(msgs) != 1 {
			t.Fatalf("got %d messages, want 1", len(msgs))
		}
		if msgs[0].UUID != "a1" {
			t.Errorf("msgs[0].UUID = %q, want %q", msgs[0].UUID, "a1")
		}
	})

	t.Run("offset beyond length returns nil", func(t *testing.T) {
		msgs, err := GetSessionMessages("cccc-3333",
			WithSessionDirectory("/test/project"),
			WithSessionOffset(100),
		)
		if err != nil {
			t.Fatalf("GetSessionMessages() error: %v", err)
		}
		if msgs != nil {
			t.Fatalf("got %d messages, want nil", len(msgs))
		}
	})

	t.Run("not found returns error", func(t *testing.T) {
		_, err := GetSessionMessages("nonexistent", WithSessionDirectory("/test/project"))
		if err == nil {
			t.Fatal("expected error for nonexistent session")
		}
	})
}

func TestGetSessionInfo(t *testing.T) {
	_, projDir := setupTestProject(t)

	writeSessionJSONL(t, projDir, "dddd-4444", []map[string]any{
		{"type": "queue-operation", "timestamp": "2026-03-15T10:00:00Z", "sessionId": "dddd-4444"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "Analyze the code"}, "uuid": "u1", "timestamp": "2026-03-15T10:00:01Z", "cwd": "/my/project", "gitBranch": "feature-x", "sessionId": "dddd-4444"},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "I'll analyze it."}}}, "uuid": "a1", "timestamp": "2026-03-15T10:00:02Z", "sessionId": "dddd-4444"},
	})

	t.Run("returns session info", func(t *testing.T) {
		info, err := GetSessionInfo("dddd-4444", WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("GetSessionInfo() error: %v", err)
		}
		if info == nil {
			t.Fatal("expected non-nil info")
		}
		if info.SessionID != "dddd-4444" {
			t.Errorf("SessionID = %q, want %q", info.SessionID, "dddd-4444")
		}
		if info.FileSize == nil {
			t.Error("FileSize should not be nil")
		}
	})

	t.Run("extracts first prompt", func(t *testing.T) {
		info, err := GetSessionInfo("dddd-4444", WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("GetSessionInfo() error: %v", err)
		}
		if info.FirstPrompt == nil || *info.FirstPrompt != "Analyze the code" {
			t.Errorf("FirstPrompt = %v, want %q", info.FirstPrompt, "Analyze the code")
		}
	})

	t.Run("summary falls back to first prompt", func(t *testing.T) {
		info, err := GetSessionInfo("dddd-4444", WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("GetSessionInfo() error: %v", err)
		}
		if info.Summary != "Analyze the code" {
			t.Errorf("Summary = %q, want %q", info.Summary, "Analyze the code")
		}
	})

	t.Run("extracts git branch and cwd", func(t *testing.T) {
		info, err := GetSessionInfo("dddd-4444", WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("GetSessionInfo() error: %v", err)
		}
		if info.GitBranch == nil || *info.GitBranch != "feature-x" {
			t.Errorf("GitBranch = %v, want %q", info.GitBranch, "feature-x")
		}
		if info.Cwd == nil || *info.Cwd != "/my/project" {
			t.Errorf("Cwd = %v, want %q", info.Cwd, "/my/project")
		}
	})

	t.Run("extracts created_at", func(t *testing.T) {
		info, err := GetSessionInfo("dddd-4444", WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("GetSessionInfo() error: %v", err)
		}
		if info.CreatedAt == nil {
			t.Fatal("CreatedAt should not be nil")
		}
		// 2026-03-15T10:00:00Z
		if *info.CreatedAt != 1773568800000 {
			t.Errorf("CreatedAt = %d, want %d", *info.CreatedAt, 1773568800000)
		}
	})

	t.Run("not found returns nil", func(t *testing.T) {
		info, err := GetSessionInfo("nonexistent", WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("GetSessionInfo() unexpected error: %v", err)
		}
		if info != nil {
			t.Error("expected nil for nonexistent session")
		}
	})
}

func TestBuildSessionInfoSummaryPriority(t *testing.T) {
	_, projDir := setupTestProject(t)

	t.Run("custom title takes priority", func(t *testing.T) {
		writeSessionJSONL(t, projDir, "eeee-5555", []map[string]any{
			{"type": "queue-operation", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "eeee-5555"},
			{"type": "user", "message": map[string]any{"role": "user", "content": "Original prompt"}, "uuid": "u1", "timestamp": "2026-01-01T00:00:01Z", "sessionId": "eeee-5555"},
			{"type": "custom-title", "customTitle": "My Custom Title", "sessionId": "eeee-5555"},
		})

		info, err := GetSessionInfo("eeee-5555", WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if info.Summary != "My Custom Title" {
			t.Errorf("Summary = %q, want %q", info.Summary, "My Custom Title")
		}
		if info.CustomTitle == nil || *info.CustomTitle != "My Custom Title" {
			t.Errorf("CustomTitle = %v, want %q", info.CustomTitle, "My Custom Title")
		}
	})

	t.Run("falls back to session ID when no prompt", func(t *testing.T) {
		writeSessionJSONL(t, projDir, "ffff-6666", []map[string]any{
			{"type": "queue-operation", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "ffff-6666"},
		})

		info, err := GetSessionInfo("ffff-6666", WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if info.Summary != "ffff-6666" {
			t.Errorf("Summary = %q, want %q", info.Summary, "ffff-6666")
		}
	})
}

func TestBuildSessionInfoSkipsMetaMessages(t *testing.T) {
	_, projDir := setupTestProject(t)

	writeSessionJSONL(t, projDir, "gggg-7777", []map[string]any{
		{"type": "queue-operation", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "gggg-7777"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "system caveat message"}, "isMeta": true, "uuid": "meta1", "timestamp": "2026-01-01T00:00:01Z", "sessionId": "gggg-7777"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "Real user prompt"}, "uuid": "u1", "timestamp": "2026-01-01T00:00:02Z", "sessionId": "gggg-7777"},
	})

	info, err := GetSessionInfo("gggg-7777", WithSessionDirectory("/test/project"))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if info.FirstPrompt == nil || *info.FirstPrompt != "Real user prompt" {
		t.Errorf("FirstPrompt = %v, want %q (should skip meta)", info.FirstPrompt, "Real user prompt")
	}
}

func TestBuildSessionInfoTagClearing(t *testing.T) {
	_, projDir := setupTestProject(t)

	writeSessionJSONL(t, projDir, "hhhh-8888", []map[string]any{
		{"type": "queue-operation", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "hhhh-8888"},
		{"type": "tag", "tag": "important", "sessionId": "hhhh-8888"},
		{"type": "tag", "tag": "", "sessionId": "hhhh-8888"},
	})

	info, err := GetSessionInfo("hhhh-8888", WithSessionDirectory("/test/project"))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if info.Tag != nil {
		t.Errorf("Tag = %v, want nil (should be cleared)", info.Tag)
	}
}

func TestParseJSONLSkipsMalformedLines(t *testing.T) {
	_, projDir := setupTestProject(t)

	path := filepath.Join(projDir, "malformed.jsonl")
	content := `{"type":"user","uuid":"u1","message":{"role":"user","content":"hello"},"timestamp":"2026-01-01T00:00:00Z"}
not valid json
{"type":"assistant","uuid":"a1","message":{"role":"assistant","content":[]},"timestamp":"2026-01-01T00:00:01Z"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	entries, err := parseJSONLFile(path)
	if err != nil {
		t.Fatalf("parseJSONLFile() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (should skip malformed)", len(entries))
	}
}
