package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/roadmap"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

// hookInput は Claude Code hook が stdin に渡す JSON のうち touch が使う項目。
type hookInput struct {
	SessionID        string `json:"session_id"`
	HookEventName    string `json:"hook_event_name"`
	NotificationType string `json:"notification_type"`
}

// errNoChange は UpdateStore に保存不要を伝えるための番兵エラー。
var errNoChange = errors.New("no change")

var (
	touchQuick      bool
	touchTrackState bool
)

var touchCmd = &cobra.Command{
	Use:   "touch [name]",
	Short: "Update last-seen timestamp for a context",
	Long: `Update the last-seen timestamp for a context.
If called from a Claude Code hook, reads session info from stdin.
If called with a name, updates that specific context.
Use --quick to skip phase scan and milestone collection (for high-frequency hooks).
Use --track-state to record the agent state from the hook event (running / needs_input / turn_done / ended).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}

		// Read hook input before taking the store lock
		var input hookInput
		if stdinIsPipe() {
			input, err = parseHookInput(os.Stdin)
			if err != nil {
				return err
			}
		}

		var updated model.Context
		err = s.UpdateStore(func(store *model.Store) error {
			var name string
			if ctx := store.FindByProviderSession(model.ProviderClaude, input.SessionID); ctx != nil {
				name = ctx.Name
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

			// State changes are saved even when last_seen is throttled
			stateChanged := touchTrackState && applyHookState(ctx, input, now)
			seen := applyLastSeen(ctx, now, touchQuick)
			if !seen && !stateChanged {
				return errNoChange
			}

			if seen && !touchQuick {
				// Auto-detect phase (fast mode for hook performance)
				phaseScanner := roadmap.NewScanner()
				phaseScanner.RefreshPhase(ctx, roadmap.ScanModeFast)

				// Collect git milestones
				collectAndSaveMilestones(s, ctx)

				// Record session_end event
				recordEvent(s, ctx.Name, model.MilestoneSessionEnd, "")
			}
			updated = *ctx
			return nil
		})
		if errors.Is(err, errNoChange) {
			return nil // silently skip
		}
		if err != nil {
			return err
		}

		fmt.Printf("Updated [%s] last-seen to %s (total: %s)\n", updated.Name, updated.LastSeen.Format(time.RFC3339), formatDuration(updated.TotalTime))
		return nil
	},
}

// parseHookInput は hook の stdin JSON（1 行）を読む。空入力はゼロ値を返す。
func parseHookInput(r io.Reader) (hookInput, error) {
	var input hookInput
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return input, nil
	}
	if err := json.Unmarshal(scanner.Bytes(), &input); err != nil {
		return input, fmt.Errorf("failed to parse hook input: %w", err)
	}
	return input, nil
}

// applyLastSeen は last_seen と累計時間を更新する。quick のときは 5 分以内の更新を間引き、false を返す。
func applyLastSeen(ctx *model.Context, now time.Time, quick bool) bool {
	if quick && !ctx.LastSeen.IsZero() && now.Sub(ctx.LastSeen) < 5*time.Minute {
		return false
	}

	// Only count if last seen was within the last hour (active session)
	if !ctx.LastSeen.IsZero() {
		elapsed := now.Sub(ctx.LastSeen)
		if elapsed > 0 && elapsed < time.Hour {
			ctx.TotalTime += elapsed
		}
	}
	ctx.LastSeen = now
	return true
}

// applyHookState は hook イベントからエージェント状態を更新する。状態が変わらなければ false を返す。
// 同じ状態の再記録を避けるのは、UserPromptSubmit など高頻度の hook で毎回 store を書き換えないため。
func applyHookState(ctx *model.Context, input hookInput, now time.Time) bool {
	next, ok := model.AgentStateFromHook(input.HookEventName, input.NotificationType, ctx.AgentState)
	if !ok || next == ctx.AgentState {
		return false
	}
	ctx.SetAgentState(next, now)
	return true
}

func init() {
	touchCmd.Flags().BoolVar(&touchQuick, "quick", false, "Quick mode: only update last-seen and total time (skip phase scan and milestones)")
	touchCmd.Flags().BoolVar(&touchTrackState, "track-state", false, "Record agent state from the hook event read from stdin")
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
