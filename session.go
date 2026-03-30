package claudecode

import (
	"github.com/severity1/claude-agent-sdk-go/internal/session"
)

// SDKSessionInfo holds metadata about a session.
type SDKSessionInfo = session.SDKSessionInfo

// SessionMessage represents a message from a session transcript.
type SessionMessage = session.SessionMessage

// SessionOption configures session query behavior.
type SessionOption = session.SessionOption

// WithSessionDirectory scopes the query to a specific project directory.
// When omitted, sessions across all projects are searched.
var WithSessionDirectory = session.WithSessionDirectory

// WithSessionLimit sets the maximum number of results to return.
var WithSessionLimit = session.WithSessionLimit

// WithSessionOffset skips the first n messages (GetSessionMessages only).
var WithSessionOffset = session.WithSessionOffset

// WithIncludeWorktrees controls whether sessions from git worktree paths
// are included when directory is inside a git repository. Default is true.
var WithIncludeWorktrees = session.WithIncludeWorktrees

// ListSessions returns metadata for sessions, sorted by LastModified descending.
// Use WithSessionDirectory to scope to a specific project, or omit to list all.
//
// Example:
//
//	// List 10 most recent sessions in a project
//	sessions, err := claudecode.ListSessions(
//	    claudecode.WithSessionDirectory("/path/to/project"),
//	    claudecode.WithSessionLimit(10),
//	)
//
//	// List all sessions across all projects
//	sessions, err := claudecode.ListSessions()
func ListSessions(opts ...SessionOption) ([]SDKSessionInfo, error) {
	return session.ListSessions(opts...)
}

// GetSessionMessages reads user and assistant messages from a session transcript.
//
// Example:
//
//	messages, err := claudecode.GetSessionMessages(sessionID,
//	    claudecode.WithSessionDirectory("/path/to/project"),
//	    claudecode.WithSessionLimit(20),
//	)
func GetSessionMessages(sessionID string, opts ...SessionOption) ([]SessionMessage, error) {
	return session.GetSessionMessages(sessionID, opts...)
}

// GetSessionInfo returns metadata for a single session by ID.
// Returns nil (not an error) if the session is not found.
//
// Example:
//
//	info, err := claudecode.GetSessionInfo(sessionID)
//	if info != nil {
//	    fmt.Println(info.Summary)
//	}
func GetSessionInfo(sessionID string, opts ...SessionOption) (*SDKSessionInfo, error) {
	return session.GetSessionInfo(sessionID, opts...)
}
