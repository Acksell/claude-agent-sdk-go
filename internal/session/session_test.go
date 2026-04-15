package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Test constants for repeated string literals (goconst).
const (
	roleUser      = "user"
	roleAssistant = "assistant"
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
// The encoded directory name is computed dynamically so tests work on Windows
// where filepath.Abs("/test/project") prepends a drive letter.
func setupTestProject(t *testing.T) (configDir string, projectDir string) {
	t.Helper()
	cfgDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)

	abs, err := filepath.Abs("/test/project")
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	encoded := encodeCwd(abs)
	projDir := filepath.Join(cfgDir, "projects", encoded)
	if err := os.MkdirAll(projDir, 0o750); err != nil {
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
	return path
}

// textContent creates a content block array matching the real CLI format:
// [{"type": "text", "text": "..."}]
func textContent(text string) []any {
	return []any{map[string]any{"type": "text", "text": text}}
}

func TestListSessions(t *testing.T) {
	_, projDir := setupTestProject(t)

	writeSessionJSONL(t, projDir, "aaaa-1111", []map[string]any{
		{"type": "queue-operation", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "aaaa-1111"},
		{"type": "user", "message": map[string]any{"role": "user", "content": textContent("Hello world")}, "uuid": "u1", "timestamp": "2026-01-01T00:00:01Z", "cwd": "/test/project", "gitBranch": "main", "sessionId": "aaaa-1111"},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": textContent("Hi!")}, "uuid": "a1", "timestamp": "2026-01-01T00:00:02Z", "sessionId": "aaaa-1111"},
	})

	writeSessionJSONL(t, projDir, "bbbb-2222", []map[string]any{
		{"type": "queue-operation", "timestamp": "2026-02-01T00:00:00Z", "sessionId": "bbbb-2222"},
		{"type": "user", "message": map[string]any{"role": "user", "content": textContent("Fix the bug")}, "uuid": "u2", "timestamp": "2026-02-01T00:00:01Z", "cwd": "/test/project", "gitBranch": "fix-branch", "sessionId": "bbbb-2222"},
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

func TestGetMessages(t *testing.T) {
	_, projDir := setupTestProject(t)

	writeSessionJSONL(t, projDir, "cccc-3333", []map[string]any{
		{"type": "queue-operation", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "cccc-3333"},
		{"type": "user", "message": map[string]any{"role": "user", "content": textContent("Hello")}, "uuid": "u1", "timestamp": "2026-01-01T00:00:01Z", "sessionId": "cccc-3333"},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": textContent("Hi!")}, "uuid": "a1", "timestamp": "2026-01-01T00:00:02Z", "sessionId": "cccc-3333"},
		{"type": "user", "message": map[string]any{"role": "user", "content": textContent("Thanks")}, "uuid": "u2", "timestamp": "2026-01-01T00:00:03Z", "sessionId": "cccc-3333"},
		{"type": "last-prompt", "lastPrompt": "Thanks", "sessionId": "cccc-3333"},
	})

	t.Run("returns user and assistant messages", func(t *testing.T) {
		msgs, err := GetMessages("cccc-3333", WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("GetMessages() error: %v", err)
		}
		if len(msgs) != 3 {
			t.Fatalf("got %d messages, want 3", len(msgs))
		}
		if msgs[0].Type != roleUser {
			t.Errorf("msgs[0].Type = %q, want %q", msgs[0].Type, roleUser)
		}
		if msgs[1].Type != roleAssistant {
			t.Errorf("msgs[1].Type = %q, want %q", msgs[1].Type, roleAssistant)
		}
		if msgs[2].Type != roleUser {
			t.Errorf("msgs[2].Type = %q, want %q", msgs[2].Type, roleUser)
		}
	})

	t.Run("preserves uuid and session_id", func(t *testing.T) {
		msgs, err := GetMessages("cccc-3333", WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("GetMessages() error: %v", err)
		}
		if msgs[0].UUID != "u1" {
			t.Errorf("msgs[0].UUID = %q, want %q", msgs[0].UUID, "u1")
		}
		if msgs[0].SessionID != "cccc-3333" {
			t.Errorf("msgs[0].SessionID = %q, want %q", msgs[0].SessionID, "cccc-3333")
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		msgs, err := GetMessages("cccc-3333",
			WithSessionDirectory("/test/project"),
			WithSessionLimit(2),
		)
		if err != nil {
			t.Fatalf("GetMessages() error: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("got %d messages, want 2", len(msgs))
		}
	})

	t.Run("respects offset", func(t *testing.T) {
		msgs, err := GetMessages("cccc-3333",
			WithSessionDirectory("/test/project"),
			WithSessionOffset(1),
		)
		if err != nil {
			t.Fatalf("GetMessages() error: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("got %d messages, want 2", len(msgs))
		}
		if msgs[0].Type != roleAssistant {
			t.Errorf("msgs[0].Type = %q, want %q (after offset=1)", msgs[0].Type, roleAssistant)
		}
	})

	t.Run("offset beyond length returns nil", func(t *testing.T) {
		msgs, err := GetMessages("cccc-3333",
			WithSessionDirectory("/test/project"),
			WithSessionOffset(100),
		)
		if err != nil {
			t.Fatalf("GetMessages() error: %v", err)
		}
		if msgs != nil {
			t.Fatalf("got %d messages, want nil", len(msgs))
		}
	})

	t.Run("not found returns error", func(t *testing.T) {
		_, err := GetMessages("nonexistent", WithSessionDirectory("/test/project"))
		if err == nil {
			t.Fatal("expected error for nonexistent session")
		}
	})
}

func TestGetSessionInfo(t *testing.T) {
	_, projDir := setupTestProject(t)

	writeSessionJSONL(t, projDir, "dddd-4444", []map[string]any{
		{"type": "queue-operation", "timestamp": "2026-03-15T10:00:00Z", "sessionId": "dddd-4444"},
		{"type": "user", "message": map[string]any{"role": "user", "content": textContent("Analyze the code")}, "uuid": "u1", "timestamp": "2026-03-15T10:00:01Z", "cwd": "/my/project", "gitBranch": "feature-x", "sessionId": "dddd-4444"},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": textContent("I'll analyze it.")}, "uuid": "a1", "timestamp": "2026-03-15T10:00:02Z", "sessionId": "dddd-4444"},
	})

	t.Run("summary uses timestamp fallback", func(t *testing.T) {
		info, _ := GetSessionInfo("dddd-4444", WithSessionDirectory("/test/project"))
		want := "New session - 2026-03-15T10:00:00Z"
		if info.Summary != want {
			t.Errorf("Summary = %q, want %q", info.Summary, want)
		}
	})

	t.Run("extracts git branch and cwd", func(t *testing.T) {
		info, _ := GetSessionInfo("dddd-4444", WithSessionDirectory("/test/project"))
		if info.GitBranch == nil || *info.GitBranch != "feature-x" {
			t.Errorf("GitBranch = %v, want %q", info.GitBranch, "feature-x")
		}
		if info.Cwd == nil || *info.Cwd != "/my/project" {
			t.Errorf("Cwd = %v, want %q", info.Cwd, "/my/project")
		}
	})

	t.Run("not found returns nil", func(t *testing.T) {
		info, err := GetSessionInfo("nonexistent", WithSessionDirectory("/test/project"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
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
			{"type": "user", "message": map[string]any{"role": "user", "content": textContent("Original prompt")}, "uuid": "u1", "timestamp": "2026-01-01T00:00:01Z", "sessionId": "eeee-5555"},
			{"type": "custom-title", "customTitle": "My Custom Title", "sessionId": "eeee-5555"},
		})
		info, _ := GetSessionInfo("eeee-5555", WithSessionDirectory("/test/project"))
		if info.Summary != "My Custom Title" {
			t.Errorf("Summary = %q, want %q", info.Summary, "My Custom Title")
		}
	})

	t.Run("falls back to timestamp when no title", func(t *testing.T) {
		writeSessionJSONL(t, projDir, "ffff-6666", []map[string]any{
			{"type": "queue-operation", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "ffff-6666"},
		})
		info, _ := GetSessionInfo("ffff-6666", WithSessionDirectory("/test/project"))
		want := "New session - 2026-01-01T00:00:00Z"
		if info.Summary != want {
			t.Errorf("Summary = %q, want %q", info.Summary, want)
		}
	})

	t.Run("falls back to session ID when no timestamp", func(t *testing.T) {
		writeSessionJSONL(t, projDir, "ffff-7777", []map[string]any{
			{"type": "queue-operation", "sessionId": "ffff-7777"},
		})
		info, _ := GetSessionInfo("ffff-7777", WithSessionDirectory("/test/project"))
		if info.Summary != "ffff-7777" {
			t.Errorf("Summary = %q, want %q", info.Summary, "ffff-7777")
		}
	})
}

func TestMessageContentParsing(t *testing.T) {
	_, projDir := setupTestProject(t)

	writeSessionJSONL(t, projDir, "content-test", []map[string]any{
		{"type": "user", "message": map[string]any{"role": "user", "content": "raw string prompt"}, "uuid": "u1", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "content-test"},
		{"type": "user", "message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "hello from blocks"},
		}}, "uuid": "u2", "timestamp": "2026-01-01T00:00:01Z", "sessionId": "content-test"},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "thinking", "thinking": "let me think", "signature": "sig123"},
			map[string]any{"type": "text", "text": "Here is my answer"},
		}}, "uuid": "a1", "timestamp": "2026-01-01T00:00:02Z", "sessionId": "content-test"},
	})

	msgs, err := GetMessages("content-test", WithSessionDirectory("/test/project"))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}

	t.Run("string content", func(t *testing.T) {
		mc := msgs[0].Content
		if mc == nil {
			t.Fatal("MessageContent is nil")
		}
		if mc.Kind != ContentTypeString {
			t.Fatalf("Kind = %d, want ContentTypeString", mc.Kind)
		}
		if mc.String != "raw string prompt" {
			t.Errorf("String = %q, want %q", mc.String, "raw string prompt")
		}
	})

	t.Run("text content block", func(t *testing.T) {
		mc := msgs[1].Content
		if mc == nil {
			t.Fatal("MessageContent is nil")
		}
		if mc.Kind != ContentTypeBlocks {
			t.Fatalf("Kind = %d, want ContentTypeBlocks", mc.Kind)
		}
		if len(mc.Blocks) != 1 {
			t.Fatalf("got %d blocks, want 1", len(mc.Blocks))
		}
		b := mc.Blocks[0]
		if b.Type != "text" {
			t.Errorf("Type = %q, want %q", b.Type, "text")
		}
		if b.Text != "hello from blocks" {
			t.Errorf("Text = %q, want %q", b.Text, "hello from blocks")
		}
	})

	t.Run("thinking + text blocks", func(t *testing.T) {
		mc := msgs[2].Content
		if mc == nil {
			t.Fatal("MessageContent is nil")
		}
		if len(mc.Blocks) != 2 {
			t.Fatalf("got %d blocks, want 2", len(mc.Blocks))
		}
		thinking := mc.Blocks[0]
		if thinking.Type != "thinking" {
			t.Errorf("Type = %q, want %q", thinking.Type, "thinking")
		}
		if thinking.Thinking != "let me think" {
			t.Errorf("Thinking = %q, want %q", thinking.Thinking, "let me think")
		}
		if thinking.Signature != "sig123" {
			t.Errorf("Signature = %q, want %q", thinking.Signature, "sig123")
		}
		text := mc.Blocks[1]
		if text.Text != "Here is my answer" {
			t.Errorf("Text = %q, want %q", text.Text, "Here is my answer")
		}
	})
}

func TestContentBlockParsing(t *testing.T) {
	_, projDir := setupTestProject(t)

	writeSessionJSONL(t, projDir, "blocks-test", []map[string]any{
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "toolu_123", "name": "Read", "input": map[string]any{"path": "/foo"}},
		}}, "uuid": "a1", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "blocks-test"},
		{"type": "user", "message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "toolu_123", "content": "file contents here"},
		}}, "uuid": "u1", "timestamp": "2026-01-01T00:00:01Z", "sessionId": "blocks-test"},
		{"type": "user", "message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "abc123"}},
		}}, "uuid": "u2", "timestamp": "2026-01-01T00:00:02Z", "sessionId": "blocks-test"},
	})

	msgs, err := GetMessages("blocks-test", WithSessionDirectory("/test/project"))
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	t.Run("tool_use block", func(t *testing.T) {
		b := msgs[0].Content.Blocks[0]
		if b.Type != "tool_use" {
			t.Errorf("Type = %q, want %q", b.Type, "tool_use")
		}
		if b.ID != "toolu_123" {
			t.Errorf("ID = %q, want %q", b.ID, "toolu_123")
		}
		if b.Name != "Read" {
			t.Errorf("Name = %q, want %q", b.Name, "Read")
		}
		if b.Input["path"] != "/foo" {
			t.Errorf("Input[path] = %v, want %q", b.Input["path"], "/foo")
		}
	})

	t.Run("tool_result block", func(t *testing.T) {
		b := msgs[1].Content.Blocks[0]
		if b.Type != "tool_result" {
			t.Errorf("Type = %q, want %q", b.Type, "tool_result")
		}
		if b.ToolUseID != "toolu_123" {
			t.Errorf("ToolUseID = %q, want %q", b.ToolUseID, "toolu_123")
		}
		if b.Content != "file contents here" {
			t.Errorf("Content = %v, want %q", b.Content, "file contents here")
		}
	})

	t.Run("image block", func(t *testing.T) {
		b := msgs[2].Content.Blocks[0]
		if b.Type != "image" {
			t.Errorf("Type = %q, want %q", b.Type, "image")
		}
		if b.Source["type"] != "base64" {
			t.Errorf("Source.type = %v, want %q", b.Source["type"], "base64")
		}
	})

	t.Run("Raw is always populated", func(t *testing.T) {
		for i, m := range msgs {
			for j, b := range m.Content.Blocks {
				if b.Raw == nil {
					t.Errorf("msgs[%d].Blocks[%d].Raw is nil", i, j)
				}
			}
		}
	})
}

func TestContentBlockUnknownType(t *testing.T) {
	_, projDir := setupTestProject(t)

	writeSessionJSONL(t, projDir, "unknown-test", []map[string]any{
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "some_future_type", "foo": "bar", "baz": 42.0},
		}}, "uuid": "a1", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "unknown-test"},
	})

	msgs, err := GetMessages("unknown-test", WithSessionDirectory("/test/project"))
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	b := msgs[0].Content.Blocks[0]
	if b.Type != "some_future_type" {
		t.Errorf("Type = %q, want %q", b.Type, "some_future_type")
	}
	if b.Raw["foo"] != "bar" {
		t.Errorf("Raw[foo] = %v, want %q", b.Raw["foo"], "bar")
	}
	if b.Raw["baz"] != 42.0 {
		t.Errorf("Raw[baz] = %v, want 42.0", b.Raw["baz"])
	}
}

func TestBuildSessionInfoTagClearing(t *testing.T) {
	_, projDir := setupTestProject(t)

	writeSessionJSONL(t, projDir, "hhhh-8888", []map[string]any{
		{"type": "queue-operation", "timestamp": "2026-01-01T00:00:00Z", "sessionId": "hhhh-8888"},
		{"type": "tag", "tag": "important", "sessionId": "hhhh-8888"},
		{"type": "tag", "tag": "", "sessionId": "hhhh-8888"},
	})

	info, _ := GetSessionInfo("hhhh-8888", WithSessionDirectory("/test/project"))
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
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
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
