package cmd

import (
	"fmt"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

var archiveCmd = &cobra.Command{
	Use:   "archive <n>",
	Short: "Archive a context (move to done status)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		s, err := storage.New()
		if err != nil {
			return err
		}

		alreadyDone := false
		_, err = updateContext(s, name, func(ctx *model.Context) error {
			if ctx.Status == model.StatusDone {
				alreadyDone = true
				return storage.ErrSkipSave
			}
			ctx.Status = model.StatusDone
			return nil
		})
		if err != nil {
			return err
		}
		if alreadyDone {
			fmt.Printf("Context [%s] is already archived\n", name)
			return nil
		}

		fmt.Printf("✓ Archived [%s]\n", name)
		return nil
	},
}

var removeCmd = &cobra.Command{
	Use:     "remove <n>",
	Aliases: []string{"rm"},
	Short:   "Remove a context from tracking",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		s, err := storage.New()
		if err != nil {
			return err
		}
		_, removed, err := removeContexts(s, name)
		if err != nil {
			return err
		}
		if removed == 0 {
			return fmt.Errorf("context [%s] not found", name)
		}

		fmt.Printf("✓ Removed [%s]\n", name)
		return nil
	},
}
