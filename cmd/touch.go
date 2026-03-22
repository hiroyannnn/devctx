package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/roadmap"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

type SessionEndInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	Reason         string `json:"reason"` // "exit", "timeout", etc.
}

var touchQuick bool

var touchCmd = &cobra.Command{
	Use:   "touch [name]",
	Short: "Update last-seen timestamp for a context",
	Long: `Update the last-seen timestamp for a context.
If called from a Claude Code hook, reads session info from stdin.
If called with a name, updates that specific context.
Use --quick to skip phase scan and milestone collection (for high-frequency hooks).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		var name string

		// Check if stdin has data (called from hook)
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			// Reading from pipe (hook mode)
			var input SessionEndInput
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				if err := json.Unmarshal(scanner.Bytes(), &input); err != nil {
					return fmt.Errorf("failed to parse hook input: %w", err)
				}
				// Find by session ID
				ctx := store.FindBySessionID(input.SessionID)
				if ctx != nil {
					name = ctx.Name
				}
			}
		}

		// If name provided as argument, use that
		if len(args) > 0 {
			name = args[0]
		}

		if name == "" {
			return fmt.Errorf("no context specified and no session ID found")
		}

		ctx := store.FindByName(name)
		if ctx == nil {
			return fmt.Errorf("context [%s] not found", name)
		}

		now := time.Now()

		// Quick mode: skip if last_seen is within 5 minutes (throttle)
		if touchQuick && !ctx.LastSeen.IsZero() {
			if now.Sub(ctx.LastSeen) < 5*time.Minute {
				return nil // silently skip
			}
		}

		// Calculate session time and add to total
		// Only count if last seen was within the last hour (active session)
		if !ctx.LastSeen.IsZero() {
			elapsed := now.Sub(ctx.LastSeen)
			if elapsed > 0 && elapsed < time.Hour {
				ctx.TotalTime += elapsed
			}
		}

		ctx.LastSeen = now

		if !touchQuick {
			// Auto-detect phase (fast mode for hook performance)
			phaseScanner := roadmap.NewScanner()
			phaseScanner.RefreshPhase(ctx, roadmap.ScanModeFast)

			// Collect git milestones
			collectAndSaveMilestones(s, ctx)

			// Record session_end event
			recordEvent(s, ctx.Name, model.MilestoneSessionEnd, "")
		}

		if err := s.SaveStore(store); err != nil {
			return err
		}

		fmt.Printf("Updated [%s] last-seen to %s (total: %s)\n", name, ctx.LastSeen.Format(time.RFC3339), formatDuration(ctx.TotalTime))
		return nil
	},
}

func init() {
	touchCmd.Flags().BoolVar(&touchQuick, "quick", false, "Quick mode: only update last-seen and total time (skip phase scan and milestones)")
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "0m"
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
