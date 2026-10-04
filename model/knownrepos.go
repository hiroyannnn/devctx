package model

import "sort"

// ScanRepos は contexts と islands に現れる repo（重複なし・昇順）と、repo ごとのアクティブな context 数を 1 回の走査で返す。
// done の context も repo としては含める（完了済みでも island に束ねたい repo は残るため）が、件数には数えない。
// is は LoadIslands で正規化済みであること。ここでは再正規化しない（RepoKey も正規化済み）。
func ScanRepos(store *Store, is *IslandStore) ([]string, map[string]int) {
	seen := map[string]bool{}
	var out []string
	active := map[string]int{}
	add := func(root string) {
		if root != "" && !seen[root] {
			seen[root] = true
			out = append(out, root)
		}
	}
	for _, c := range store.Contexts {
		key := RepoKey(c)
		add(key)
		if key != "" && c.Status != StatusDone {
			active[key]++
		}
	}
	for _, rn := range is.Repos {
		add(rn.Root)
	}
	// 親としてだけ参照される repo（contexts も repo ノードも無い）も一覧に入れる。
	// Why: CLI の list が Web（親 repo を薄いノードで補う）と同じ木を出せるように
	addParentRepo := func(parent string) {
		if kind, v, err := ParseRef(parent); err == nil && kind == RefRepo {
			add(v)
		}
	}
	for _, isl := range is.Islands {
		addParentRepo(isl.Parent)
	}
	for _, rn := range is.Repos {
		addParentRepo(rn.Parent)
	}
	sort.Strings(out)
	return out, active
}

// KnownRepos は ScanRepos の repo 一覧だけを返す。
// Why: CLI の解決と Web の編集 API が「知っている repo」の定義を共有し、片方だけ通る ref を作らないため。
func KnownRepos(store *Store, is *IslandStore) []string {
	repos, _ := ScanRepos(store, is)
	return repos
}
