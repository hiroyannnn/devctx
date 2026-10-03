package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

// hookInput は Claude Code hook が stdin に渡す JSON のうち touch が使う項目。
type hookInput struct {
	SessionID        string `json:"session_id"`
	HookEventName    string `json:"hook_event_name"`
	NotificationType string `json:"notification_type"`
}

var (
	touchQuick      bool
	touchTrackState bool
	touchProvider   string
)

var touchCmd = &cobra.Command{
	Use:   "touch [name]",
	Short: "Update last-seen timestamp for a context",
	Long: `Update the last-seen timestamp for a context.
If called from a Claude Code / Codex hook (--provider), reads session info from stdin.
If called with a name, updates that specific context.
Use --quick to skip phase scan and milestone collection (for high-frequency hooks).
Use --track-state to record the agent state from the hook event (running / needs_input / turn_done / ended).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		provider, err := model.ParseProvider(touchProvider)
		if err != nil {
			return err
		}
		s, err := storage.New()
		if err != nil {
			return err
		}

		// Read hook input before taking the store lock
		var input hookInput
		fromHook := stdinIsPipe()
		if fromHook {
			input, err = parseHookInput(os.Stdin)
			if err != nil {
				return err
			}
		}

		var updated model.Context
		var seen bool
		err = s.UpdateStore(func(store *model.Store) error {
			name := resolveTouchTarget(store, provider, input.SessionID, args)
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
			seen = applyLastSeen(ctx, now, touchQuick)
			if !seen && !stateChanged {
				return storage.ErrSkipSave
			}
			updated = *ctx
			return nil
		})
		if err != nil {
			return err
		}
		if updated.Name == "" {
			return nil // silently skip: nothing changed
		}

		if seen && !touchQuick {
			if err := refreshPhaseOutsideLock(s, updated); err != nil {
				return err
			}
			// Collect git milestones
			collectAndSaveMilestones(s, &updated)

			// Record session_end event
			recordEvent(s, updated.Name, model.MilestoneSessionEnd, "")
		}

		// Why not print from hooks: Claude Code adds UserPromptSubmit stdout to the model's context
		if !fromHook {
			fmt.Printf("Updated [%s] last-seen to %s (total: %s)\n", updated.Name, updated.LastSeen.Format(time.RFC3339), formatDuration(updated.TotalTime))
		}
		return nil
	},
}

// resolveTouchTarget は touch が更新する context の名前を返す。明示された名前を優先し、
// なければ hook の session_id を provider 側のセッション ID として探す。見つからなければ空文字。
func resolveTouchTarget(store *model.Store, provider model.Provider, sessionID string, args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	if ctx := store.FindByProviderSession(provider, sessionID); ctx != nil {
		return ctx.Name
	}
	return ""
}

// parseHookInput は hook の stdin JSON（1 行）を読む。空入力はゼロ値を返す。
func parseHookInput(r io.Reader) (hookInput, error) {
	var input hookInput
	err := decodeHookInput(r, &input)
	return input, err
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
	touchCmd.Flags().StringVar(&touchProvider, "provider", "claude", "Agent provider that owns the hook session (claude/codex/manual)")
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
