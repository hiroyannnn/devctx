package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

var (
	insightGoal    string
	insightFocus   string
	insightNext    string
	insightState   string
)

var insightCmd = &cobra.Command{
	Use:   "insight [name]",
	Short: "Manually set session insights (goal, focus, next step)",
	Long: `Set or update AI-inferred fields for a session manually.
This overrides any auto-generated insights.

Without a name argument, uses the context matching the current directory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}
		insights, err := s.LoadInsights()
		if err != nil {
			return err
		}

		// Find target context
		var ctx *model.Context
		if len(args) > 0 {
			ctx = store.FindByName(args[0])
			if ctx == nil {
				return fmt.Errorf("context [%s] not found", args[0])
			}
		} else {
			cwd, _ := os.Getwd()
			worktreeRoot := getWorktreeRoot(cwd)
			if worktreeRoot != "" {
				cwd = worktreeRoot
			}
			ctx = store.FindByWorktree(cwd)
			if ctx == nil {
				return fmt.Errorf("no context found for current directory\nSpecify a name as argument")
			}
		}

		// Get or create insight
		existing := insights.Get(ctx.Name)
		var insight model.SessionInsight
		if existing != nil {
			insight = *existing
		} else {
			insight.Name = ctx.Name
		}

		// Apply flags
		updated := false
		if insightGoal != "" {
			insight.Goal = insightGoal
			updated = true
		}
		if insightFocus != "" {
			insight.CurrentFocus = insightFocus
			updated = true
		}
		if insightNext != "" {
			insight.NextStep = insightNext
			updated = true
		}
		if insightState != "" {
			state := model.AttentionState(insightState)
			switch state {
			case model.AttentionActive, model.AttentionWaiting, model.AttentionIdle, model.AttentionBlocked:
				insight.AttentionState = state
				updated = true
			default:
				return fmt.Errorf("invalid attention state: %q (must be active/waiting/idle/blocked)", insightState)
			}
		}

		if !updated {
			// Show current insight
			if existing == nil {
				fmt.Printf("[%s] No insights set.\n", ctx.Name)
				fmt.Println("Use --goal, --focus, --next, --state flags to set them.")
				return nil
			}
			fmt.Printf("[%s]\n", ctx.Name)
			fmt.Printf("  Goal:  %s\n", existing.Goal)
			fmt.Printf("  Focus: %s\n", existing.CurrentFocus)
			fmt.Printf("  Next:  %s\n", existing.NextStep)
			fmt.Printf("  State: %s\n", existing.AttentionState)
			fmt.Printf("  Updated: %s\n", existing.InferredAt.Format(time.RFC3339))
			return nil
		}

		insight.InferredAt = time.Now()
		insights.Set(insight)

		if err := s.SaveInsights(insights); err != nil {
			return err
		}

		fmt.Printf("Updated insight for [%s]\n", ctx.Name)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(insightCmd)

	insightCmd.Flags().StringVar(&insightGoal, "goal", "", "Session goal (what it's trying to achieve)")
	insightCmd.Flags().StringVar(&insightFocus, "focus", "", "Current focus (what's being worked on now)")
	insightCmd.Flags().StringVar(&insightNext, "next", "", "Next step (what should be done next)")
	insightCmd.Flags().StringVar(&insightState, "state", "", "Attention state (active/waiting/idle/blocked)")
}
