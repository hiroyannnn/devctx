package cmd

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hiroyannnn/devctx/model"
)

// claudeAdapter は ~/.claude/projects の transcript から Claude Code セッションを探す。
type claudeAdapter struct{}

func (claudeAdapter) Provider() model.Provider { return model.ProviderClaude }

func (claudeAdapter) AgentCommand(ctx model.Context) (string, error) {
	if ctx.SessionID != "" {
		return "claude --resume " + shellQuote(ctx.SessionID), nil
	}
	return "claude", nil
}

func (claudeAdapter) Discover(store *model.Store) ([]DiscoveredSession, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	claudeDir := filepath.Join(home, ".claude", "projects")
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		return nil, nil
	}

	var sessions []DiscoveredSession

	// Walk through project directories
	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		projectHash := entry.Name()
		projectDir := filepath.Join(claudeDir, projectHash)

		// Find transcript files
		transcripts, err := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
		if err != nil {
			continue
		}

		for _, transcriptPath := range transcripts {
			info, err := os.Stat(transcriptPath)
			if err != nil {
				continue
			}

			sessionID := strings.TrimSuffix(filepath.Base(transcriptPath), ".jsonl")

			sessions = append(sessions, DiscoveredSession{
				Provider:       model.ProviderClaude,
				SessionID:      sessionID,
				SessionName:    extractSessionName(transcriptPath),
				TranscriptPath: transcriptPath,
				ProjectPath:    extractProjectPath(transcriptPath),
				ProjectHash:    projectHash,
				LastModified:   info.ModTime(),
				MessageCount:   countMessages(transcriptPath),
				IsRegistered:   store.FindByProviderSession(model.ProviderClaude, sessionID) != nil,
			})
		}
	}

	// Sort by last modified (most recent first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastModified.After(sessions[j].LastModified)
	})

	return sessions, nil
}
