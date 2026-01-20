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
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		ctx := store.FindByName(name)
		if ctx == nil {
			return fmt.Errorf("context [%s] not found", name)
		}

		if ctx.Status == model.StatusDone {
			fmt.Printf("Context [%s] is already archived\n", name)
			return nil
		}

		ctx.Status = model.StatusDone

		if err := s.SaveStore(store); err != nil {
			return err
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
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		if !store.Remove(name) {
			return fmt.Errorf("context [%s] not found", name)
		}

		if err := s.SaveStore(store); err != nil {
			return err
		}

		fmt.Printf("✓ Removed [%s]\n", name)
		return nil
	},
}
