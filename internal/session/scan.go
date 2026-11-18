package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// SessionInfo holds session information to scan from JSON.
type SessionInfo struct {
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
}

// ScanSessionNames scans the base directory (or ~/.claude/sessions if
// baseDir is empty) to find session names assigned by the agent.
// Returns a map of sessionId → name.
// Handles errors gracefully: if the directory doesn't exist or a file is
// broken, returns an empty map.
func ScanSessionNames(baseDir string) map[string]string {
	result := make(map[string]string)

	// If baseDir is empty, use the default ~/.claude/sessions.
	if baseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return result
		}
		baseDir = filepath.Join(home, ".claude", "sessions")
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		// Directory doesn't exist or can't be read, return an empty map.
		return result
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only process .json files.
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(baseDir, entry.Name())
		if sessionID, name, ok := parseSessionFile(path); ok {
			result[sessionID] = name
		}
	}

	return result
}

// parseSessionFile reads a JSON file and extracts sessionId + name.
// Returns (sessionId, name, ok).
func parseSessionFile(path string) (string, string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}

	var info SessionInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return "", "", false
	}

	// Trim whitespace and check it's not empty.
	sessionID := strings.TrimSpace(info.SessionID)
	name := strings.TrimSpace(info.Name)

	if sessionID == "" || name == "" {
		return "", "", false
	}

	return sessionID, name, true
}
