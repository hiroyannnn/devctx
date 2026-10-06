package cmd

import (
	"encoding/json"
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
	ToolName         string `json:"tool_name"`
	// ToolInput は待ち要求の分類にだけ使う。Write の content やパッチ全文のように巨大でも、構造体には展開しない
	ToolInput json.RawMessage `json:"tool_input"`
	// Prompt は UserPromptSubmit のプロンプト本文。タスク marker の検出にだけ使い、保存しない
	Prompt string `json:"prompt"`
}

// maxHookInputBytes は hook stdin の読み取り上限。tool_input は巨大になりうるので、
// 上限を超えたら黙って切り詰めず不正入力として扱う（状態を変えない）。
const maxHookInputBytes = 8 << 20

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
		// Why capture before the lock: async hooks run concurrently and may acquire the store lock
		// out of order; the process start time is the closest available proxy for the event time
		eventTime := time.Now()

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

		// Why unlocked read first: 高頻度 hook の大半は no-op（同じ状態・quick の間引き）で、
		// ロック待ちと書き込みを避けたい。store の書き込みは atomic rename なので、ロック無しの読み取りでも
		// 中途半端なファイルは見えない。古い値を読んでも、変化ありと判断すれば下のロック内で再評価される
		if touchIsNoop(s, provider, input, args, eventTime, touchQuick, touchTrackState) {
			return nil
		}

		updated, seen, err := touchOnce(s, provider, input, args, eventTime, touchQuick, touchTrackState)
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

// touchOnce は store をロックして、hook イベント 1 件分の更新（状態・last_seen・タスク紐付け）を保存する。
// 何も変わらなければ updated は ゼロ値（Name が空）。
func touchOnce(s *storage.Storage, provider model.Provider, input hookInput, args []string, eventTime time.Time, quick, trackState bool) (updated model.Context, seen bool, err error) {
	// Why islands をロックの外で読む: UpdateStore の中で UpdateIslands を呼ぶとロックの取り順が入れ子になる。
	// タスクの実在確認は読み取りだけでよく、古い値を読んでも「無いタスクへは付けない」側に倒れるだけ
	markerID, hasMarker := taskMarkerFromInput(input)
	validMarker := hasMarker && taskExists(s, markerID)

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
		stateChanged := trackState && applyHookState(ctx, input, eventTime)
		linked := validMarker && model.ApplyTaskMarker(ctx, input.SessionID, markerID, eventTime)
		seen = applyLastSeen(ctx, now, quick)
		if !seen && !stateChanged && !linked {
			return storage.ErrSkipSave
		}
		updated = *ctx
		return nil
	})
	return updated, seen, err
}

// taskMarkerFromInput は UserPromptSubmit のプロンプトから marker のタスク id を取り出す。
// プロンプトを持つのは UserPromptSubmit だけだが、イベント名も見て他のイベントの偶然の prompt 項目を拾わない。
func taskMarkerFromInput(input hookInput) (string, bool) {
	if input.HookEventName != "UserPromptSubmit" {
		return "", false
	}
	return model.ParseTaskMarker(input.Prompt)
}

// taskExists は id が実在するタスクかを islands.yaml から（ロック無しで）確かめる。読めなければ false。
func taskExists(s *storage.Storage, id string) bool {
	is, err := s.LoadIslands()
	if err != nil {
		return false
	}
	return is.HasTask(id)
}

// touchIsNoop は store をロック無しで読み、この touch が何も変えないことが確実なら true を返す。
// 読み込み失敗・対象不明などは false を返し、エラー報告を UpdateStore 側に任せる。
func touchIsNoop(s *storage.Storage, provider model.Provider, input hookInput, args []string, eventTime time.Time, quick, trackState bool) bool {
	// marker 付きのプロンプトは、リンクが変わるか否かをロック内の ApplyTaskMarker に決めさせる。
	// Why: ここで状態だけを見て no-op と判断すると、同じ running 状態の 2 つ目以降のプロンプトで付け替えを取りこぼす
	if _, ok := taskMarkerFromInput(input); ok {
		return false
	}
	store, err := s.LoadStore()
	if err != nil {
		return false
	}
	name := resolveTouchTarget(store, provider, input.SessionID, args)
	if name == "" {
		return false
	}
	ctx := store.FindByName(name)
	if ctx == nil {
		return false
	}
	probe := *ctx // 副作用を本物の store に残さないためコピーで評価する
	stateChanged := trackState && applyHookState(&probe, input, eventTime)
	seen := applyLastSeen(&probe, time.Now(), quick)
	return !stateChanged && !seen
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
	err := decodeHookInput(io.LimitReader(r, maxHookInputBytes), &input)
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

// applyHookState は hook イベントからエージェント状態と待ち要求を更新する。変化が無ければ false を返す。
// ロック無しの no-op 判定（touchIsNoop）とロック内の更新が同じ関数を通るので、判定がずれない。
// 同じ状態の再記録を避けるのは、UserPromptSubmit など高頻度の hook で毎回 store を書き換えないため。
// eventTime より後に記録された状態があれば、遅れて届いた古いイベントとして捨てる（async hook の順序逆転対策）。
func applyHookState(ctx *model.Context, input hookInput, eventTime time.Time) bool {
	if eventTime.Before(ctx.AgentStateAt) {
		return false
	}
	return model.ApplyHookEvent(ctx, model.HookEvent{
		Name:             input.HookEventName,
		NotificationType: input.NotificationType,
		ToolName:         input.ToolName,
		ToolInput:        input.ToolInput,
	}, eventTime)
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
