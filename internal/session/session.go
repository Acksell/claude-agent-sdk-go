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
	GitBranch    *string `json:"git_branch,omitempty"`
	Cwd          *string `json:"cwd,omitempty"`
	Tag          *string `json:"tag,omitempty"`
	CreatedAt    *int64  `json:"created_at,omitempty"`
}

// ContentType discriminates the MessageContent union.
type ContentType int

const (
	// ContentTypeString indicates the message content is a plain string.
	ContentTypeString ContentType = iota + 1
	// ContentTypeBlocks indicates the message content is an array of content blocks.
	ContentTypeBlocks
)

// MessageContent is a sum type representing the content of a session message.
// Kind indicates which field is populated.
type MessageContent struct {
	Kind   ContentType
	String string         // populated when Kind == ContentTypeString
	Blocks []ContentBlock // populated when Kind == ContentTypeBlocks
}

// Block type constants for ContentBlock.Type.
const (
	BlockTypeText                         = "text"
	BlockTypeThinking                     = "thinking"
	BlockTypeRedactedThinking             = "redacted_thinking"
	BlockTypeToolUse                      = "tool_use"
	BlockTypeServerToolUse                = "server_tool_use"
	BlockTypeToolResult                   = "tool_result"
	BlockTypeImage                        = "image"
	BlockTypeWebSearchToolResult          = "web_search_tool_result"
	BlockTypeWebFetchToolResult           = "web_fetch_tool_result"
	BlockTypeCodeExecutionToolResult      = "code_execution_tool_result"
	BlockTypeBashCodeExecutionToolResult  = "bash_code_execution_tool_result"
	BlockTypeTextEditorCodeExecToolResult = "text_editor_code_execution_tool_result"
	BlockTypeToolSearchToolResult         = "tool_search_tool_result"
	BlockTypeContainerUpload              = "container_upload"
)

// ContentBlock represents a typed content block from a session message.
// The Type field discriminates the variant. Unknown types are preserved in Raw.
type ContentBlock struct {
	// Type discriminates the block variant.
	// Use the BlockType* constants to compare against known types.
	Type string `json:"type"`

	// Raw holds the full original map for all block types (always populated).
	Raw map[string]any `json:"-"`

	// text
	Text string `json:"text,omitempty"`

	// thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	// redacted_thinking
	Data string `json:"data,omitempty"`

	// tool_use, server_tool_use
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`

	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   any    `json:"content,omitempty"` // string or nested blocks
	IsError   *bool  `json:"is_error,omitempty"`

	// image
	Source map[string]any `json:"source,omitempty"`
}

// Message represents a message from a session transcript.
type Message struct {
	Type      string `json:"type"` // "user", "assistant", etc. (there are many other types beyond just these two)
	UUID      string `json:"uuid"`
	SessionID string `json:"session_id"`
	// *should* be true for system-injected messages,
	// but nothing in claude api enforces it,
	// so some implementations like claude-vscode inject messages without this.
	IsMeta          bool            `json:"is_meta"`
	RawMessage      map[string]any  `json:"message"`                      // raw message data
	Content         *MessageContent `json:"-"`                            // parsed content
	ParentToolUseID *string         `json:"parent_tool_use_id,omitempty"` // reserved
}

// Option configures session query behavior.
type Option func(*sessionOpts)

type sessionOpts struct {
	directory string
	limit     int
	offset    int
}

func defaultOpts() sessionOpts {
	return sessionOpts{}
}

// WithSessionDirectory scopes the query to a specific project directory.
// When omitted, sessions across all projects are searched.
func WithSessionDirectory(dir string) Option {
	return func(o *sessionOpts) {
		o.directory = dir
	}
}

// WithSessionLimit sets the maximum number of results to return.
func WithSessionLimit(n int) Option {
	return func(o *sessionOpts) {
		o.limit = n
	}
}

// WithSessionOffset skips the first n messages (GetMessages only).
func WithSessionOffset(n int) Option {
	return func(o *sessionOpts) {
		o.offset = n
	}
}

// ListSessions returns metadata for sessions, sorted by LastModified descending.
func ListSessions(opts ...Option) ([]SDKSessionInfo, error) {
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

// GetMessages reads user and assistant messages from a session transcript.
func GetMessages(sessionID string, opts ...Option) ([]Message, error) {
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

	messages := buildMessages(sessionID, entries)

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
func GetSessionInfo(sessionID string, opts ...Option) (*SDKSessionInfo, error) {
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
	path = filepath.Clean(path)
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
func parseJSONLFile(path string) (entries []jsonlEntry, err error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("closing session file: %w", cerr)
		}
	}()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var raw map[string]any
		if uerr := json.Unmarshal(line, &raw); uerr != nil {
			continue // skip malformed lines
		}
		typ, _ := raw["type"].(string)
		entries = append(entries, jsonlEntry{entryType: typ, raw: raw})
	}
	if serr := scanner.Err(); serr != nil {
		return nil, fmt.Errorf("scanning JSONL file: %w", serr)
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

	// Summary priority: custom_title > timestamp fallback
	switch {
	case info.CustomTitle != nil:
		info.Summary = *info.CustomTitle
	case info.CreatedAt != nil:
		info.Summary = fmt.Sprintf("New session - %s", time.UnixMilli(*info.CreatedAt).UTC().Format(time.RFC3339))
	default:
		info.Summary = sessionID
	}

	return info
}

// parseMessageContent parses the content field of a message into a typed MessageContent.
func parseMessageContent(msg map[string]any) *MessageContent {
	content, ok := msg["content"]
	if !ok {
		return nil
	}

	// String content.
	if s, ok := content.(string); ok {
		return &MessageContent{
			Kind:   ContentTypeString,
			String: s,
		}
	}

	// Content block array.
	blocks, ok := content.([]any)
	if !ok {
		return nil
	}

	parsed := make([]ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		b, ok := block.(map[string]any)
		if !ok {
			continue
		}
		parsed = append(parsed, parseContentBlock(b))
	}

	return &MessageContent{
		Kind:   ContentTypeBlocks,
		Blocks: parsed,
	}
}

// parseContentBlock parses a single content block from a raw map.
// Known fields are extracted into typed struct fields; Raw is always populated.
func parseContentBlock(raw map[string]any) ContentBlock {
	cb := ContentBlock{
		Raw: raw,
	}

	cb.Type, _ = raw["type"].(string)

	switch cb.Type {
	case BlockTypeText:
		cb.Text, _ = raw["text"].(string)

	case BlockTypeThinking:
		cb.Thinking, _ = raw["thinking"].(string)
		cb.Signature, _ = raw["signature"].(string)

	case BlockTypeRedactedThinking:
		cb.Data, _ = raw["data"].(string)

	case BlockTypeToolUse, BlockTypeServerToolUse:
		cb.ID, _ = raw["id"].(string)
		cb.Name, _ = raw["name"].(string)
		if input, ok := raw["input"].(map[string]any); ok {
			cb.Input = input
		}

	case BlockTypeToolResult:
		cb.ToolUseID, _ = raw["tool_use_id"].(string)
		cb.Content = raw["content"]
		if isErr, ok := raw["is_error"].(bool); ok {
			cb.IsError = &isErr
		}

	case BlockTypeImage:
		if source, ok := raw["source"].(map[string]any); ok {
			cb.Source = source
		}
	}

	return cb
}

// buildMessages extracts user and assistant messages from JSONL entries.
func buildMessages(sessionID string, entries []jsonlEntry) []Message {
	var messages []Message
	for _, e := range entries {
		if e.entryType != "user" && e.entryType != "assistant" {
			continue
		}

		msg := Message{
			Type:      e.entryType,
			SessionID: sessionID,
		}
		if uuid, ok := e.raw["uuid"].(string); ok {
			msg.UUID = uuid
		}
		if meta, ok := e.raw["isMeta"].(bool); ok && meta {
			msg.IsMeta = true
		}
		if rawMsg, ok := e.raw["message"].(map[string]any); ok {
			msg.RawMessage = rawMsg
			msg.Content = parseMessageContent(rawMsg)
		}

		messages = append(messages, msg)
	}
	return messages
}
