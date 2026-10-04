package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

var islandCmd = &cobra.Command{
	Use:   "island",
	Short: "Group repos and themes into hand-made islands (shown in the All Projects mind map)",
	Long: `An island is a theme node you create by hand (e.g. 人事強化). Islands and repos
form one tree: a repo can sit under an island, and an island can sit under a repo.
Agent sessions attach to their repo automatically.

Refs are island:<id> or repo:<path>. A bare name matches an island id, else a
repo basename; if it matches more than one, devctx lists the candidates and stops.
"repo:." means the repo of the current directory.

Examples:
  devctx island add 人事強化 --id hr
  devctx island attach devctx --to hr
  devctx island attach island:m3 --to repo:.
  devctx island list`,
}

var (
	islandAddID      string
	islandAddParent  string
	islandAttachTo   string
	islandRmReparent bool
)

var islandAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add an island",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		return islandAdd(s, cmd.OutOrStdout(), newRefResolver(), args[0], islandAddID, islandAddParent)
	},
}

var islandRenameCmd = &cobra.Command{
	Use:   "rename <id> <name>",
	Short: "Rename an island (its id stays the same)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		return islandRename(s, cmd.OutOrStdout(), args[0], args[1])
	},
}

var islandRmCmd = &cobra.Command{
	Use:   "rm <id>",
	Short: "Remove an island",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		return islandRm(s, cmd.OutOrStdout(), args[0], islandRmReparent)
	},
}

var islandAttachCmd = &cobra.Command{
	Use:   "attach <child> --to <parent>",
	Short: "Put an island or repo under another island or repo",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		return islandAttach(s, cmd.OutOrStdout(), newRefResolver(), args[0], islandAttachTo)
	},
}

var islandDetachCmd = &cobra.Command{
	Use:   "detach <child>",
	Short: "Move an island or repo back to the top level",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		return islandDetach(s, cmd.OutOrStdout(), newRefResolver(), args[0])
	},
}

var islandListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show the island / repo tree",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		return islandList(s, cmd.OutOrStdout())
	},
}

func init() {
	rootCmd.AddCommand(islandCmd)
	islandCmd.AddCommand(islandAddCmd, islandRenameCmd, islandRmCmd, islandAttachCmd, islandDetachCmd, islandListCmd)

	islandAddCmd.Flags().StringVar(&islandAddID, "id", "", "Island id (default: slug of the name; required for non-ASCII names you want to refer to)")
	islandAddCmd.Flags().StringVar(&islandAddParent, "parent", "", "Parent ref (island:<id> or repo:<path>)")
	islandAttachCmd.Flags().StringVar(&islandAttachTo, "to", "", "Parent ref (island:<id>, repo:<path>, or a bare name)")
	_ = islandAttachCmd.MarkFlagRequired("to")
	islandRmCmd.Flags().BoolVar(&islandRmReparent, "reparent", false, "Move children to the removed island's parent instead of refusing")
}

// 以下は cobra から切り離した本体。テストは隔離 HOME の Storage を渡して実行する。

// withIslands は islands.yaml を更新ロック内で編集する。resolver は contexts を最初に必要とした時点まで読まない。
func withIslands(s *storage.Storage, base refResolver, fn func(is *model.IslandStore, r refResolver) error) error {
	return s.UpdateIslands(func(is *model.IslandStore) error {
		return fn(is, base.bind(s, is))
	})
}

// islandID は rename / rm の引数から island の id を取り出す。型付き ref は island 種別だけを受ける。
// Why not 素の文字列から "island:" を剥がすだけにする: "repo:/x" を渡されたときに無関係なエラーになるため。
func islandID(arg string) (string, error) {
	kind, v, err := model.ParseRef(arg)
	if err != nil {
		return arg, nil // 素の id
	}
	if kind != model.RefIsland {
		return "", fmt.Errorf("%s is not an island ref (want island:<id> or a bare id)", arg)
	}
	return v, nil
}

func islandAdd(s *storage.Storage, out io.Writer, base refResolver, name, id, parent string) error {
	return withIslands(s, base, func(is *model.IslandStore, r refResolver) error {
		if parent != "" {
			var err error
			if parent, err = r.resolve(parent); err != nil {
				return err
			}
		}
		added, err := is.AddIsland(name, id, parent)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "✓ Added island %s [%s]\n", added.Name, model.IslandRef(added.ID))
		return nil
	})
}

func islandRename(s *storage.Storage, out io.Writer, idArg, name string) error {
	id, err := islandID(idArg)
	if err != nil {
		return err
	}
	return s.UpdateIslands(func(is *model.IslandStore) error {
		if err := is.RenameIsland(id, name); err != nil {
			return err
		}
		fmt.Fprintf(out, "✓ Renamed %s to %s\n", model.IslandRef(id), name)
		return nil
	})
}

func islandRm(s *storage.Storage, out io.Writer, idArg string, reparent bool) error {
	id, err := islandID(idArg)
	if err != nil {
		return err
	}
	return s.UpdateIslands(func(is *model.IslandStore) error {
		if err := is.RemoveIsland(id, reparent); err != nil {
			return err
		}
		fmt.Fprintf(out, "✓ Removed %s\n", model.IslandRef(id))
		return nil
	})
}

func islandAttach(s *storage.Storage, out io.Writer, base refResolver, childArg, parentArg string) error {
	return withIslands(s, base, func(is *model.IslandStore, r refResolver) error {
		child, err := r.resolve(childArg)
		if err != nil {
			return err
		}
		parent, err := r.resolve(parentArg)
		if err != nil {
			return err
		}
		if err := is.SetParent(child, parent); err != nil {
			return err
		}
		fmt.Fprintf(out, "✓ Attached %s under %s\n", child, parent)
		return nil
	})
}

func islandDetach(s *storage.Storage, out io.Writer, base refResolver, childArg string) error {
	return withIslands(s, base, func(is *model.IslandStore, r refResolver) error {
		child, err := r.resolve(childArg)
		if err != nil {
			return err
		}
		if err := is.Detach(child); err != nil {
			return err
		}
		fmt.Fprintf(out, "✓ Detached %s\n", child)
		return nil
	})
}

func islandList(s *storage.Storage, out io.Writer) error {
	store, err := s.LoadStore()
	if err != nil {
		return err
	}
	is, err := s.LoadIslands()
	if err != nil {
		return err
	}
	repos, active := scanRepos(store, is)
	renderIslandTree(out, is, repos, active)
	if err := is.Validate(); err != nil {
		fmt.Fprintf(out, "warning: %v\n", err)
	}
	return nil
}

// renderIslandTree はトップレベル（親なし、または親が実在しない）の island → repo の順に木を描く。
// 親を持たない repo も、既知なら全て出す（island に未接続の repo も一覧で見えるように）。
func renderIslandTree(out io.Writer, is *model.IslandStore, repos []string, active map[string]int) {
	names := map[string]string{}
	for _, i := range is.Islands {
		names[model.IslandRef(i.ID)] = fmt.Sprintf("%s [%s]", i.Name, model.IslandRef(i.ID))
	}
	label := func(ref string) string {
		if l, ok := names[ref]; ok {
			return l
		}
		_, root, _ := model.ParseRef(ref)
		return fmt.Sprintf("%s (%s, %d active)", filepath.Base(root), root, active[root])
	}

	var roots []string
	for _, i := range is.Islands {
		if ref := model.IslandRef(i.ID); is.ParentOf(ref) == "" {
			roots = append(roots, ref)
		}
	}
	sortedRepos := append([]string(nil), repos...)
	sort.Strings(sortedRepos)
	for _, root := range sortedRepos {
		if ref := model.RepoRef(root); is.ParentOf(ref) == "" {
			roots = append(roots, ref)
		}
	}

	visited := map[string]bool{}
	var walk func(ref, prefix string)
	walk = func(ref, prefix string) {
		kids := is.Children(ref)
		for i, k := range kids {
			branch, next := "├── ", "│   "
			if i == len(kids)-1 {
				branch, next = "└── ", "    "
			}
			fmt.Fprintf(out, "%s%s%s\n", prefix, branch, label(k))
			if !visited[k] { // 手編集で壊れた循環でも止める
				visited[k] = true
				walk(k, prefix+next)
			}
		}
	}
	for _, ref := range roots {
		fmt.Fprintln(out, label(ref))
		visited[ref] = true
		walk(ref, "")
	}
}
