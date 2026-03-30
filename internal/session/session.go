// Package session provides functions for reading Claude Code session data from disk.
//
// Sessions are stored as JSONL files at ~/.claude/projects/<encoded-cwd>/<session-id>.jsonl.
// The encoded-cwd replaces every non-alphanumeric character with "-".
package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SDKSessionInfo holds metadata about a session.
type SDKSessionInfo struct {
	SessionID    string  `json:"session_id"`
	Summary      string  `json:"summary"`
	LastModified int64   `json:"last_modified"`
	FileSize     *int64  `json:"file_size,omitempty"`
	CustomTitle  *string `json:"custom_title,omitempty"`
	FirstPrompt  *string `json:"first_prompt,omitempty"`
	GitBranch    *string `json:"git_branch,omitempty"`
	Cwd          *string `json:"cwd,omitempty"`
	Tag          *string `json:"tag,omitempty"`
	CreatedAt    *int64  `json:"created_at,omitempty"`
}

// SessionMessage represents a message from a session transcript.
type SessionMessage struct {
	Type            string  `json:"type"`
	UUID            string  `json:"uuid"`
	SessionID       string  `json:"session_id"`
	Message         any     `json:"message"`
	ParentToolUseID *string `json:"parent_tool_use_id,omitempty"`
}

// SessionOption configures session query behavior.
type SessionOption func(*sessionOpts)

type sessionOpts struct {
	directory        string
	limit            int
	offset           int
	includeWorktrees bool
}

func defaultOpts() sessionOpts {
	return sessionOpts{
		includeWorktrees: true,
	}
}

// WithSessionDirectory scopes the query to a specific project directory.
// When omitted, sessions across all projects are searched.
func WithSessionDirectory(dir string) SessionOption {
	return func(o *sessionOpts) {
		o.directory = dir
	}
}

// WithSessionLimit sets the maximum number of results to return.
func WithSessionLimit(n int) SessionOption {
	return func(o *sessionOpts) {
		o.limit = n
	}
}

// WithSessionOffset skips the first n messages (GetSessionMessages only).
func WithSessionOffset(n int) SessionOption {
	return func(o *sessionOpts) {
		o.offset = n
	}
}

// WithIncludeWorktrees controls whether sessions from git worktree paths
// are included when directory is inside a git repository. Default is true.
func WithIncludeWorktrees(include bool) SessionOption {
	return func(o *sessionOpts) {
		o.includeWorktrees = include
	}
}

// ListSessions returns metadata for sessions, sorted by LastModified descending.
func ListSessions(opts ...SessionOption) ([]SDKSessionInfo, error) {
	o := defaultOpts()
	for _, fn := range opts {
		fn(&o)
	}

	dirs, err := projectDirsForOpts(o)
	if err != nil {
		return nil, err
	}

	var sessions []SDKSessionInfo
	for _, dir := range dirs {
		infos, err := listSessionsInDir(dir)
		if err != nil {
			continue // skip unreadable directories
		}
		sessions = append(sessions, infos...)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastModified > sessions[j].LastModified
	})

	if o.limit > 0 && len(sessions) > o.limit {
		sessions = sessions[:o.limit]
	}

	return sessions, nil
}

// GetSessionMessages reads user and assistant messages from a session transcript.
func GetSessionMessages(sessionID string, opts ...SessionOption) ([]SessionMessage, error) {
	o := defaultOpts()
	for _, fn := range opts {
		fn(&o)
	}

	path, err := findSessionFile(sessionID, o)
	if err != nil {
		return nil, err
	}

	entries, err := parseJSONLFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading session %s: %w", sessionID, err)
	}

	messages := buildSessionMessages(sessionID, entries)

	if o.offset > 0 {
		if o.offset >= len(messages) {
			return nil, nil
		}
		messages = messages[o.offset:]
	}

	if o.limit > 0 && len(messages) > o.limit {
		messages = messages[:o.limit]
	}

	return messages, nil
}

// GetSessionInfo returns metadata for a single session by ID.
// Returns nil (not an error) if the session is not found.
func GetSessionInfo(sessionID string, opts ...SessionOption) (*SDKSessionInfo, error) {
	o := defaultOpts()
	for _, fn := range opts {
		fn(&o)
	}

	path, err := findSessionFile(sessionID, o)
	if err != nil {
		return nil, nil // session not found
	}

	return buildSessionInfoFromFile(sessionID, path)
}

// configDir returns the Claude configuration directory.
func configDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

// encodeCwd encodes a directory path by replacing non-alphanumeric characters with "-".
func encodeCwd(cwd string) string {
	var b strings.Builder
	b.Grow(len(cwd))
	for _, r := range cwd {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// projectDirsForOpts returns the project directories to search based on options.
func projectDirsForOpts(o sessionOpts) ([]string, error) {
	cfgDir, err := configDir()
	if err != nil {
		return nil, err
	}
	projectsDir := filepath.Join(cfgDir, "projects")

	if o.directory != "" {
		abs, err := filepath.Abs(o.directory)
		if err != nil {
			return nil, fmt.Errorf("resolving directory: %w", err)
		}
		encoded := encodeCwd(abs)
		dir := filepath.Join(projectsDir, encoded)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return nil, nil
		}
		return []string{dir}, nil
	}

	// List all project directories
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading projects directory: %w", err)
	}

	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(projectsDir, e.Name()))
		}
	}
	return dirs, nil
}

// listSessionsInDir lists all sessions in a single project directory.
func listSessionsInDir(dir string) ([]SDKSessionInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var sessions []SDKSessionInfo
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(name, ".jsonl")
		path := filepath.Join(dir, name)

		info, err := buildSessionInfoFromFile(sessionID, path)
		if err != nil {
			continue // skip unreadable sessions
		}
		sessions = append(sessions, *info)
	}
	return sessions, nil
}

// findSessionFile locates the JSONL file for a session ID.
func findSessionFile(sessionID string, o sessionOpts) (string, error) {
	dirs, err := projectDirsForOpts(o)
	if err != nil {
		return "", err
	}

	filename := sessionID + ".jsonl"
	for _, dir := range dirs {
		path := filepath.Join(dir, filename)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("session not found: %s", sessionID)
}

// buildSessionInfoFromFile builds SDKSessionInfo by reading a JSONL file.
func buildSessionInfoFromFile(sessionID, path string) (*SDKSessionInfo, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	entries, err := parseJSONLFile(path)
	if err != nil {
		return nil, err
	}

	return buildSessionInfo(sessionID, entries, fileInfo), nil
}

// jsonlEntry represents a parsed line from a JSONL file.
type jsonlEntry struct {
	entryType string
	raw       map[string]any
}

// parseJSONLFile reads and parses all lines from a JSONL file.
func parseJSONLFile(path string) ([]jsonlEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []jsonlEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(line, &raw); err != nil {
			continue // skip malformed lines
		}
		typ, _ := raw["type"].(string)
		entries = append(entries, jsonlEntry{entryType: typ, raw: raw})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning JSONL file: %w", err)
	}
	return entries, nil
}

// buildSessionInfo constructs SDKSessionInfo from parsed JSONL entries.
func buildSessionInfo(sessionID string, entries []jsonlEntry, fileInfo os.FileInfo) *SDKSessionInfo {
	info := &SDKSessionInfo{
		SessionID:    sessionID,
		LastModified: fileInfo.ModTime().UnixMilli(),
	}

	fileSize := fileInfo.Size()
	info.FileSize = &fileSize

	for _, e := range entries {
		switch e.entryType {
		case "custom-title":
			if title, ok := e.raw["customTitle"].(string); ok && title != "" {
				info.CustomTitle = &title
			}
		case "tag":
			if tag, ok := e.raw["tag"].(string); ok {
				if tag == "" {
					info.Tag = nil // cleared
				} else {
					info.Tag = &tag
				}
			}
		case "user":
			if info.FirstPrompt == nil && !isMeta(e.raw) {
				if msg, ok := e.raw["message"].(map[string]any); ok {
					if content, ok := msg["content"].(string); ok && content != "" {
						info.FirstPrompt = &content
					}
				}
			}
			// Track cwd and gitBranch (last one wins)
			if branch, ok := e.raw["gitBranch"].(string); ok && branch != "" {
				info.GitBranch = &branch
			}
			if cwd, ok := e.raw["cwd"].(string); ok && cwd != "" {
				info.Cwd = &cwd
			}
		}

		// CreatedAt from first entry with a timestamp
		if info.CreatedAt == nil {
			if ts, ok := e.raw["timestamp"].(string); ok && ts != "" {
				if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
					ms := t.UnixMilli()
					info.CreatedAt = &ms
				}
			}
		}
	}

	// Summary priority: custom_title > first_prompt > session_id
	switch {
	case info.CustomTitle != nil:
		info.Summary = *info.CustomTitle
	case info.FirstPrompt != nil:
		info.Summary = *info.FirstPrompt
	default:
		info.Summary = sessionID
	}

	return info
}

// isMeta checks if a JSONL entry is a meta/system message (not a real user prompt).
func isMeta(raw map[string]any) bool {
	if meta, ok := raw["isMeta"].(bool); ok && meta {
		return true
	}
	return false
}

// buildSessionMessages extracts user and assistant messages from JSONL entries.
func buildSessionMessages(sessionID string, entries []jsonlEntry) []SessionMessage {
	var messages []SessionMessage
	for _, e := range entries {
		if e.entryType != "user" && e.entryType != "assistant" {
			continue
		}
		msg := SessionMessage{
			Type:      e.entryType,
			SessionID: sessionID,
			Message:   e.raw["message"],
		}
		if uuid, ok := e.raw["uuid"].(string); ok {
			msg.UUID = uuid
		}
		messages = append(messages, msg)
	}
	return messages
}
