package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

var skipChecklist bool

var moveCmd = &cobra.Command{
	Use:   "move <n> <status>",
	Short: "Move context to a different status",
	Long: `Move a context to a new status (in-progress, review, blocked, done).
If the target status has a checklist, you'll be prompted to confirm each item.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		targetStatus := model.Status(args[1])

		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}
		config, err := s.LoadConfig()
		if err != nil {
			return err
		}

		ctx := store.FindByName(name)
		if ctx == nil {
			return fmt.Errorf("context [%s] not found", name)
		}

		// Validate status transition
		currentStatusConfig := findStatusConfig(config, ctx.Status)
		if currentStatusConfig == nil {
			return fmt.Errorf("unknown current status: %s", ctx.Status)
		}

		if !isValidTransition(currentStatusConfig, targetStatus) {
			return fmt.Errorf("cannot move from %s to %s (allowed: %v)",
				ctx.Status, targetStatus, currentStatusConfig.Next)
		}

		// Get target status config for checklist
		targetStatusConfig := findStatusConfig(config, targetStatus)
		if targetStatusConfig != nil && len(targetStatusConfig.Checklist) > 0 && !skipChecklist {
			fmt.Printf("Moving [%s] to %s\n", name, targetStatus)
			fmt.Println("Please confirm checklist items:")
			fmt.Println()

			pendingItems := 0
			for _, item := range targetStatusConfig.Checklist {
				current, exists := ctx.Checklist[item]
				if exists && current {
					fmt.Printf("  ☑ %s (already done)\n", item)
					continue
				}

				response := promptYesNo(fmt.Sprintf("  %s executed?", item))
				ctx.Checklist[item] = response
				if !response {
					pendingItems++
				}
			}

			if pendingItems > 0 {
				fmt.Printf("\n⚠ Warning: %d checklist item(s) not completed.\n", pendingItems)
				if !promptYesNo("Continue anyway?") {
					fmt.Println("Aborted.")
					return nil
				}
			}
		}

		// Update status
		ctx.Status = targetStatus
		ctx.LastSeen = time.Now()

		if err := s.SaveStore(store); err != nil {
			return err
		}

		fmt.Printf("✓ Moved [%s] to %s\n", name, targetStatus)

		if targetStatusConfig != nil && targetStatusConfig.Archive {
			fmt.Println("  (This context is now archived)")
		}

		return nil
	},
}

func init() {
	moveCmd.Flags().BoolVar(&skipChecklist, "skip-checklist", false, "Skip checklist prompts")
}

func findStatusConfig(config *model.Config, status model.Status) *model.StatusConfig {
	for i := range config.Statuses {
		if config.Statuses[i].Name == status {
			return &config.Statuses[i]
		}
	}
	return nil
}

func isValidTransition(from *model.StatusConfig, to model.Status) bool {
	for _, next := range from.Next {
		if next == to {
			return true
		}
	}
	return false
}

func promptYesNo(prompt string) bool {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s (y/n/skip): ", prompt)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))

		switch input {
		case "y", "yes":
			return true
		case "n", "no":
			return false
		case "s", "skip":
			return false
		default:
			fmt.Println("Please enter y, n, or skip")
		}
	}
}
