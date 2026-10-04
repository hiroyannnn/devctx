package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hiroyannnn/devctx/model"
)

// refResolver は CLI の入力（型付き ref / "repo:." / 素の名前）を、保存形の型付き ref に解決する。
type refResolver struct {
	islands *model.IslandStore
	repos   []string // 既知 repo（NormalizePath 済み）
	// repoFromCwd は "repo:." のための現在ディレクトリの repo。git を呼ぶので差し替え可能にしている
	repoFromCwd func() (string, error)
}

// bind は store と islands を束ねた resolver を返す。islands は UpdateIslands のロック内のものを渡す。
func (r refResolver) bind(store *model.Store, is *model.IslandStore) refResolver {
	r.islands = is
	r.repos = knownRepos(store, is)
	return r
}

// knownRepos は contexts と islands.yaml に現れる repo を重複なし・昇順で返す。
// done の context も含める: 完了済みでも island に束ねたい repo は残るため。
func knownRepos(store *model.Store, is *model.IslandStore) []string {
	seen := map[string]bool{}
	var out []string
	add := func(root string) {
		if root = model.NormalizePath(root); root != "" && !seen[root] {
			seen[root] = true
			out = append(out, root)
		}
	}
	for _, c := range store.Contexts {
		add(model.RepoKey(c))
	}
	for _, rn := range is.Repos {
		add(rn.Root)
	}
	sort.Strings(out)
	return out
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
			if !r.hasIsland(v) {
				return "", fmt.Errorf("island %q not found", v)
			}
			return input, nil
		}
		return r.resolveRepoPath(v)
	}

	var candidates []string
	if r.hasIsland(input) {
		candidates = append(candidates, model.IslandRef(input))
	}
	for _, root := range r.repos {
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

func (r refResolver) hasIsland(id string) bool {
	for _, is := range r.islands.Islands {
		if is.ID == id {
			return true
		}
	}
	return false
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
	for _, k := range r.repos {
		if k == root {
			return model.RepoRef(root), nil
		}
	}
	// 未登録の repo でもディスク上にあれば木に載せられる（contexts が無い repo の下にテーマを置くため）
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("repo %q is not a known repo and not an existing directory", root)
	}
	return model.RepoRef(root), nil
}
