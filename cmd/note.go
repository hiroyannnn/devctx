package cmd

import (
	"fmt"
	"strings"

	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

var noteCmd = &cobra.Command{
	Use:   "note <name> [message]",
	Short: "Add or show a note for a context",
	Long: `Add a note to a context to help remember what you were working on.

Examples:
  devctx note auth "OAuth2 実装中、refresh token の処理が残っている"
  devctx note auth          # Show current note
  devctx note auth --clear  # Clear the note`,
	Args: cobra.MinimumNArgs(1),
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

		clearNote, _ := cmd.Flags().GetBool("clear")

		if clearNote {
			ctx.Note = ""
			if err := s.SaveStore(store); err != nil {
				return err
			}
			fmt.Printf("✓ Cleared note for [%s]\n", name)
			return nil
		}

		if len(args) == 1 {
			// Show current note
			if ctx.Note == "" {
				fmt.Printf("[%s] has no note\n", name)
			} else {
				fmt.Printf("[%s] 📝 %s\n", name, ctx.Note)
			}
			return nil
		}

		// Set note
		note := strings.Join(args[1:], " ")
		ctx.Note = note
		if err := s.SaveStore(store); err != nil {
			return err
		}
		fmt.Printf("✓ Note set for [%s]: %s\n", name, note)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(noteCmd)
	noteCmd.Flags().Bool("clear", false, "Clear the note")
}
