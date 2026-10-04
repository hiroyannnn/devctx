package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
)

// refResolver は CLI の入力（型付き ref / "repo:." / 素の名前）を、保存形の型付き ref に解決する。
type refResolver struct {
	islands *model.IslandStore
	// repos は既知 repo（NormalizePath 済み）。型付き island ref だけの操作で contexts.yaml を読まないよう、遅延評価にしている
	repos func() ([]string, error)
	// repoFromCwd は "repo:." のための現在ディレクトリの repo。git を呼ぶので差し替え可能にしている
	repoFromCwd func() (string, error)
	// repoFromDir は dir が git 管理下ならその repo root（worktree は本体の root）、そうでなければ ""。nil なら判定しない
	repoFromDir func(dir string) string
}

// newRefResolver は本番用の resolver（git で現在ディレクトリ・指定パスの repo を判定する）。
func newRefResolver() refResolver {
	return refResolver{repoFromCwd: currentRepo, repoFromDir: detectRepoRoot}
}

// bind は islands（UpdateIslands のロック内のもの）を束ねた resolver を返す。
// 既知 repo は最初に必要になった時点で 1 度だけ contexts を読んで作る。
func (r refResolver) bind(s *storage.Storage, is *model.IslandStore) refResolver {
	r.islands = is
	r.repos = sync.OnceValues(func() ([]string, error) {
		store, err := s.LoadStore()
		if err != nil {
			return nil, err
		}
		repos, _ := model.ScanRepos(store, is)
		return repos, nil
	})
	return r
}

// currentRepo は現在ディレクトリの repo root を返す。
func currentRepo() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root := detectRepoRoot(cwd)
	if root == "" {
		return "", fmt.Errorf("%s is not inside a git repository", cwd)
	}
	return model.NormalizePath(root), nil
}

// resolve は入力を型付き ref にする。
// 素の名前は island id 完全一致 → repo の basename の順に探し、island と repo の両方に当たる、
// または同名 basename の repo が複数ある場合は推測せず、候補を型付き ref で並べてエラーにする。
// Why not 先に当たった方を採る: 黙って別物に付けると、描画されるまで誤りに気づけない。
func (r refResolver) resolve(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("ref is empty")
	}
	if kind, v, err := model.ParseRef(input); err == nil {
		if kind == model.RefIsland {
			if !r.islands.HasIsland(v) {
				return "", fmt.Errorf("island %q not found", v)
			}
			return input, nil
		}
		return r.resolveRepoPath(v)
	}

	var candidates []string
	if r.islands.HasIsland(input) {
		candidates = append(candidates, model.IslandRef(input))
	}
	repos, err := r.repos()
	if err != nil {
		return "", err
	}
	for _, root := range repos {
		if filepath.Base(root) == input {
			candidates = append(candidates, model.RepoRef(root))
		}
	}
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("no island or repo matches %q (use island:<id> or repo:<path>)", input)
	case 1:
		return candidates[0], nil
	default:
		return "", fmt.Errorf("%q is ambiguous; specify one of: %s", input, strings.Join(candidates, ", "))
	}
}

func (r refResolver) resolveRepoPath(path string) (string, error) {
	if path == "." {
		root, err := r.repoFromCwd()
		if err != nil {
			return "", err
		}
		return model.RepoRef(root), nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	root := model.NormalizePath(abs)
	// ディスク上にあれば、未登録の repo でも木に載せられる（contexts が無い repo の下にテーマを置くため）。
	// 先にこちらを見るので、既存ディレクトリの指定では contexts を読まない
	if fi, err := os.Stat(root); err == nil && fi.IsDir() {
		// repo 内のサブディレクトリを渡されても、contexts と同じ repo root に寄せる。
		// git 管理外のディレクトリはそのまま（repo を持たないテーマの置き場にできる）
		if r.repoFromDir != nil {
			if top := r.repoFromDir(root); top != "" {
				return model.RepoRef(model.NormalizePath(top)), nil
			}
		}
		return model.RepoRef(root), nil
	}
	repos, err := r.repos()
	if err != nil {
		return "", err
	}
	for _, k := range repos {
		if k == root {
			return model.RepoRef(root), nil
		}
	}
	return "", fmt.Errorf("repo %q is not a known repo and not an existing directory", root)
}
