package cmd

import (
	"fmt"
	"io"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Hand-made task nodes under islands or repos (shown in the All Projects mind map)",
	Long: `A task is a node for one piece of work, placed under an island or a repo.
Nothing can be placed under a task. Rename, remove and move tasks with the island
commands (island rename / rm / attach); the id is t1, t2, ... and is never reused.

In the mind map, "Copy as prompt" on a task puts "<name>" and a [devctx:task:<id>]
marker line on the clipboard.

Examples:
  devctx task add 求人票を直す --to hiring
  devctx task done t1
  devctx task done t1 --undo`,
}

var (
	taskAddTo    string
	taskDoneUndo bool
)

var taskAddCmd = &cobra.Command{
	Use:   "add <name> --to <parent>",
	Short: "Add a task under an island or repo",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		return taskAdd(s, cmd.OutOrStdout(), newRefResolver(), args[0], taskAddTo)
	},
}

var taskDoneCmd = &cobra.Command{
	Use:   "done <id>",
	Short: "Mark a task as done (--undo to reopen it)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		return taskDone(s, cmd.OutOrStdout(), args[0], taskDoneUndo)
	},
}

var taskLinkCmd = &cobra.Command{
	Use:   "link <context> <task>",
	Short: "Link an agent session (context) to a task",
	Long: `Hang a context under a task in the All Projects mind map. <task> is t<n> or island:t<n>.
Linking never marks the task done. The link stays when the same worktree starts a new
session; the first [devctx:task:<id>] marker of a new session replaces it.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		return taskLink(s, cmd.OutOrStdout(), args[0], args[1])
	},
}

var taskUnlinkCmd = &cobra.Command{
	Use:   "unlink <context>",
	Short: "Remove the task link from a context",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		return taskUnlink(s, cmd.OutOrStdout(), args[0])
	},
}

func init() {
	rootCmd.AddCommand(taskCmd)
	taskCmd.AddCommand(taskAddCmd, taskDoneCmd, taskLinkCmd, taskUnlinkCmd)

	taskAddCmd.Flags().StringVar(&taskAddTo, "to", "", "Parent ref (island:<id>, repo:<path>, or a bare name)")
	_ = taskAddCmd.MarkFlagRequired("to")
	taskDoneCmd.Flags().BoolVar(&taskDoneUndo, "undo", false, "Mark the task as not done")
}

func taskAdd(s *storage.Storage, out io.Writer, base refResolver, name, to string) error {
	return withIslands(s, base, func(is *model.IslandStore, r refResolver) error {
		parent, err := r.resolve(to)
		if err != nil {
			return err
		}
		added, err := is.AddTask(name, parent)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "✓ Added task %s [%s]\n", added.Name, model.IslandRef(added.ID))
		return nil
	})
}

func taskDone(s *storage.Storage, out io.Writer, idArg string, undo bool) error {
	id, err := islandID(idArg)
	if err != nil {
		return err
	}
	return s.UpdateIslands(func(is *model.IslandStore) error {
		if err := is.SetTaskDone(id, !undo); err != nil {
			return err
		}
		if undo {
			fmt.Fprintf(out, "✓ Reopened %s\n", model.IslandRef(id))
		} else {
			fmt.Fprintf(out, "✓ Done %s\n", model.IslandRef(id))
		}
		return nil
	})
}

// taskLink は context をタスクに手動で付ける。
// Why islands をロックの外で読む: UpdateStore の中で UpdateIslands を取らない（hook の touch と同じ方針）。
func taskLink(s *storage.Storage, out io.Writer, contextName, taskArg string) error {
	id, err := islandID(taskArg)
	if err != nil {
		return err
	}
	if !taskExists(s, id) {
		return fmt.Errorf("task %q not found (use t<n> or island:t<n>)", id)
	}
	ref := model.IslandRef(id)
	err = s.UpdateStore(func(store *model.Store) error {
		ctx := store.FindByName(contextName)
		if ctx == nil {
			return fmt.Errorf("context [%s] not found", contextName)
		}
		model.LinkTask(ctx, ref, time.Now())
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "✓ Linked [%s] to %s\n", contextName, ref)
	return nil
}

// taskUnlink は context のタスク紐付けを外す。
func taskUnlink(s *storage.Storage, out io.Writer, contextName string) error {
	err := s.UpdateStore(func(store *model.Store) error {
		ctx := store.FindByName(contextName)
		if ctx == nil {
			return fmt.Errorf("context [%s] not found", contextName)
		}
		model.UnlinkTask(ctx)
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "✓ Unlinked [%s]\n", contextName)
	return nil
}
