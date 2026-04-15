// Package main demonstrates listing Claude Code sessions from disk.
//
// Sessions are persisted as JSONL files under ~/.claude/projects/.
// ListSessions reads their metadata without needing a CLI connection.
//
// Run: go run main.go
package main

import (
	"fmt"
	"log"
	"time"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

func main() {
	fmt.Println("Claude Agent SDK - List Sessions Example")
	fmt.Println("=========================================")

	// List the 10 most recent sessions across all projects.
	sessions, err := claudecode.ListSessions(
		claudecode.WithSessionLimit(10),
	)
	if err != nil {
		log.Fatalf("ListSessions failed: %v", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions found. Run a Claude query first to create one.")
		return
	}

	fmt.Printf("\nFound %d session(s):\n\n", len(sessions))
	for i, s := range sessions {
		modified := time.UnixMilli(s.LastModified).Format("2006-01-02 15:04")

		// Summary is: custom title > first prompt > session ID.
		summary := s.Summary
		if len(summary) > 80 {
			summary = summary[:77] + "..."
		}

		fmt.Printf("  %d. %s\n", i+1, summary)
		fmt.Printf("     ID: %s\n", s.SessionID)
		fmt.Printf("     Modified: %s\n", modified)
		if s.GitBranch != nil {
			fmt.Printf("     Branch: %s\n", *s.GitBranch)
		}
		if s.Cwd != nil {
			fmt.Printf("     Cwd: %s\n", *s.Cwd)
		}
		fmt.Println()
	}
}
