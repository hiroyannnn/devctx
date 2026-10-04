package model

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Island は手で作るテーマノード（例: 人事強化）。repo を持たなくてよい。
type Island struct {
	ID     string `yaml:"id" json:"id"`
	Name   string `yaml:"name" json:"name"`
	Parent string `yaml:"parent,omitempty" json:"parent"` // 型付き ref。"" はトップレベル
}

// RepoNode は repo の親を記録する。親を持つ repo だけがここに載る。
type RepoNode struct {
	Root   string `yaml:"root" json:"root"` // NormalizePath 済みの絶対パス
	Parent string `yaml:"parent,omitempty" json:"parent"`
}

// IslandStore は island と repo を 1 本の木にするための親子関係。
// island の親は island / repo のどちらでもよく、repo の親も island / repo のどちらでもよい。
type IslandStore struct {
	Islands []Island   `yaml:"islands" json:"islands"`
	Repos   []RepoNode `yaml:"repos" json:"repos"`
}

// RefKind は ref の種別。
type RefKind string

const (
	RefIsland RefKind = "island"
	RefRepo   RefKind = "repo"
)

// IslandRef は island の型付き ref を返す。
func IslandRef(id string) string { return string(RefIsland) + ":" + id }

// RepoRef は repo の型付き ref を返す。root は NormalizePath 済みであること。
func RepoRef(root string) string { return string(RefRepo) + ":" + root }

// ParseRef は "island:<id>" / "repo:<path>" を分解する。
// Why not 素の文字列も受ける: island id と repo の basename が衝突しうるので、保存形は常に型付きに固定する。
// 曖昧な入力の解決は CLI 側（resolveRef）の責務。
func ParseRef(ref string) (RefKind, string, error) {
	for _, k := range []RefKind{RefIsland, RefRepo} {
		prefix := string(k) + ":"
		if strings.HasPrefix(ref, prefix) {
			v := strings.TrimPrefix(ref, prefix)
			if v == "" {
				return "", "", fmt.Errorf("invalid ref %q: empty value", ref)
			}
			return k, v, nil
		}
	}
	return "", "", fmt.Errorf("invalid ref %q: want island:<id> or repo:<path>", ref)
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// slug は名前から ASCII の小文字英数と '-' だけの id 候補を作る。ASCII が残らなければ ""。
func slug(name string) string {
	return strings.Trim(nonSlugChars.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

func (s *IslandStore) findIsland(id string) *Island {
	for i := range s.Islands {
		if s.Islands[i].ID == id {
			return &s.Islands[i]
		}
	}
	return nil
}

// HasIsland は id の island が存在するかを返す。
func (s *IslandStore) HasIsland(id string) bool { return s.findIsland(id) != nil }

func (s *IslandStore) findRepo(root string) *RepoNode {
	for i := range s.Repos {
		if s.Repos[i].Root == root {
			return &s.Repos[i]
		}
	}
	return nil
}

// checkRefExists は ref が island なら実在することを確認する。repo は未登録でもよい。
// Why: repo は contexts が無くても木に載せたい（登録 repo か否かの判定は呼び出し側）。
func (s *IslandStore) checkRefExists(ref string) error {
	kind, v, err := ParseRef(ref)
	if err != nil {
		return err
	}
	if kind == RefIsland && !s.HasIsland(v) {
		return fmt.Errorf("island %q not found", v)
	}
	return nil
}

// AddIsland は island を追加する。id が空なら name の slug、ASCII が残らなければ island-N。
func (s *IslandStore) AddIsland(name, id, parent string) (Island, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Island{}, fmt.Errorf("island name is empty")
	}
	if parent != "" {
		if err := s.checkRefExists(parent); err != nil {
			return Island{}, err
		}
	}
	switch {
	case id != "":
		if !idPattern.MatchString(id) {
			return Island{}, fmt.Errorf("invalid island id %q: use lowercase letters, digits and '-'", id)
		}
	case slug(name) != "":
		id = slug(name)
	default:
		for n := 1; ; n++ {
			id = "island-" + strconv.Itoa(n)
			if s.findIsland(id) == nil {
				break
			}
		}
	}
	if s.findIsland(id) != nil {
		return Island{}, fmt.Errorf("island id %q already exists; pass --id to choose another", id)
	}
	is := Island{ID: id, Name: name, Parent: parent}
	s.Islands = append(s.Islands, is)
	return is, nil
}

// RenameIsland は表示名だけを変える。id は参照されているので変えない。
func (s *IslandStore) RenameIsland(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("island name is empty")
	}
	is := s.findIsland(id)
	if is == nil {
		return fmt.Errorf("island %q not found", id)
	}
	is.Name = name
	return nil
}

// RemoveIsland は island を消す。子がいる場合は reparent=true のときだけ、子を消す island の親へ付け替える。
// Why not 子ごと消す: 手で組んだ構造の巻き込み削除は取り返しがつかない。
func (s *IslandStore) RemoveIsland(id string, reparent bool) error {
	is := s.findIsland(id)
	if is == nil {
		return fmt.Errorf("island %q not found", id)
	}
	ref := IslandRef(id)
	children := s.Children(ref)
	if len(children) > 0 && !reparent {
		return fmt.Errorf("island %q has %d child(ren) (%s); use --reparent to move them up", id, len(children), strings.Join(children, ", "))
	}
	// 生の Parent ではなく解決後の親へ。dangling な親を子へ引き継がない
	newParent := s.ParentOf(ref)
	for _, c := range children {
		// 付け替え先は削除対象の親なので存在・循環の検査は不要（木の中で 1 段持ち上げるだけ）
		s.setParentUnchecked(c, newParent)
	}
	for i := range s.Islands {
		if s.Islands[i].ID == id {
			s.Islands = append(s.Islands[:i], s.Islands[i+1:]...)
			break
		}
	}
	return nil
}

func (s *IslandStore) setParentUnchecked(child, parent string) {
	kind, v, _ := ParseRef(child)
	if kind == RefIsland {
		if is := s.findIsland(v); is != nil {
			is.Parent = parent
		}
		return
	}
	s.setRepoParent(v, parent)
}

func (s *IslandStore) setRepoParent(root, parent string) {
	if parent == "" {
		for i := range s.Repos {
			if s.Repos[i].Root == root {
				s.Repos = append(s.Repos[:i], s.Repos[i+1:]...)
				return
			}
		}
		return
	}
	if rn := s.findRepo(root); rn != nil {
		rn.Parent = parent
		return
	}
	s.Repos = append(s.Repos, RepoNode{Root: root, Parent: parent})
}

// SetParent は child を parent の下に付ける。child の repo は未登録でもよく、無ければ RepoNode を作る。
// 自分自身と循環（island→repo→island も含む）は拒否する。
func (s *IslandStore) SetParent(child, parent string) error {
	if parent == "" {
		return fmt.Errorf("parent is empty; use detach to move %s to top level", child)
	}
	if err := s.checkRefExists(child); err != nil {
		return err
	}
	if err := s.checkRefExists(parent); err != nil {
		return err
	}
	if child == parent {
		return fmt.Errorf("%s cannot be attached to itself", child)
	}
	// parent から根へ辿って child に当たれば循環。手編集で既に壊れた yaml でも止まるよう visited で守る
	visited := map[string]bool{}
	for cur := parent; cur != "" && !visited[cur]; cur = s.ParentOf(cur) {
		if cur == child {
			return fmt.Errorf("attaching %s to %s would create a cycle", child, parent)
		}
		visited[cur] = true
	}
	s.setParentUnchecked(child, parent)
	return nil
}

// Detach は child をトップレベルに戻す。repo は RepoNode を消して yaml を小さく保つ。
func (s *IslandStore) Detach(child string) error {
	if err := s.checkRefExists(child); err != nil {
		return err
	}
	s.setParentUnchecked(child, "")
	return nil
}

// ParentOf は ref の親 ref を返す。トップレベル・未登録・親が実在しない（dangling）場合は ""。
// Why: 手編集や他 PR の変更で island が消えても、描画は落とさずトップレベルとして扱う。
func (s *IslandStore) ParentOf(ref string) string {
	kind, v, err := ParseRef(ref)
	if err != nil {
		return ""
	}
	var parent string
	if kind == RefIsland {
		is := s.findIsland(v)
		if is == nil {
			return ""
		}
		parent = is.Parent
	} else {
		rn := s.findRepo(v)
		if rn == nil {
			return ""
		}
		parent = rn.Parent
	}
	if s.checkRefExists(parent) != nil {
		return ""
	}
	return parent
}

// Children は ref を親とする island → repo の順の ref 一覧を返す。
func (s *IslandStore) Children(ref string) []string {
	var out []string
	for _, is := range s.Islands {
		if is.Parent == ref {
			out = append(out, IslandRef(is.ID))
		}
	}
	for _, rn := range s.Repos {
		if rn.Parent == ref {
			out = append(out, RepoRef(rn.Root))
		}
	}
	return out
}

// Resolved は dangling な親と循環を断ち切った複製を返す（receiver は変えない）。API と CLI の list が使う。
// 循環は island → repo の順にファイルの並びで処理し、輪を閉じる辺（後から処理した側の親）を ""（トップレベル）にする。
// Why: 手編集で循環した yaml でも、描画側が全ノードを root から辿れる非循環の木を受け取れるようにする。
// Why not 循環の全ノードをトップレベルに落とす: 手で組んだ構造の大半が残るよう、切る辺は 1 本に抑える。
func (s *IslandStore) Resolved() IslandStore {
	out := IslandStore{
		Islands: make([]Island, len(s.Islands)),
		Repos:   make([]RepoNode, len(s.Repos)),
	}
	accepted := map[string]string{} // 採用済みの子 → 親。ここは常に非循環
	closesLoop := func(ref, parent string) bool {
		for cur := parent; cur != ""; cur = accepted[cur] {
			if cur == ref {
				return true
			}
		}
		return false
	}
	resolve := func(ref string) string {
		parent := s.ParentOf(ref)
		if parent == "" || closesLoop(ref, parent) {
			return ""
		}
		accepted[ref] = parent
		return parent
	}
	for i, is := range s.Islands {
		is.Parent = resolve(IslandRef(is.ID))
		out.Islands[i] = is
	}
	for i, rn := range s.Repos {
		rn.Parent = resolve(RepoRef(rn.Root))
		out.Repos[i] = rn
	}
	return out
}

// Validate は解決できない親（dangling または循環）があれば全件を列挙したエラーを返す。CLI の list が警告に使う。
// 判定は Resolved と同じ（元の Parent があるのに解決後は ""）なので、API と CLI で食い違わない。
func (s *IslandStore) Validate() error {
	r := s.Resolved()
	var bad []string
	for i, is := range s.Islands {
		if is.Parent != "" && r.Islands[i].Parent == "" {
			bad = append(bad, fmt.Sprintf("%s -> %s", IslandRef(is.ID), is.Parent))
		}
	}
	for i, rn := range s.Repos {
		if rn.Parent != "" && r.Repos[i].Parent == "" {
			bad = append(bad, fmt.Sprintf("%s -> %s", RepoRef(rn.Root), rn.Parent))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("invalid parent (dangling or cyclic): %s", strings.Join(bad, "; "))
	}
	return nil
}

// Normalize は repo のパスを NormalizePath にそろえる（repo ノードの root と、island / repo の repo 型の親 ref）。
// 正規化後に同じ root になった repo ノードは先勝ちで 1 つにする（先に親が無ければ後の親を引き継ぐ）。
// 正規化の結果、自分自身の下になった親は外す。
// Why: contexts のグルーピング（RepoKey）と同じキーに読み込み時点でそろえ、CLI と Web が同じ木を見るようにする。
// 手編集で symlink 経由のパスが書かれていても突き合うようにするため。
func (s *IslandStore) Normalize() {
	for i := range s.Islands {
		s.Islands[i].Parent = normalizeRepoRef(s.Islands[i].Parent)
	}
	index := map[string]int{}
	repos := s.Repos[:0]
	for _, rn := range s.Repos {
		rn.Root = NormalizePath(rn.Root)
		rn.Parent = normalizeRepoRef(rn.Parent)
		if rn.Parent == RepoRef(rn.Root) { // symlink 経由で自分自身の下になったもの
			rn.Parent = ""
		}
		if i, dup := index[rn.Root]; dup {
			// 先勝ちだが、先の側に親が無ければ後の親を拾う（親を黙って落とさない）
			if repos[i].Parent == "" {
				repos[i].Parent = rn.Parent
			}
			continue
		}
		index[rn.Root] = len(repos)
		repos = append(repos, rn)
	}
	s.Repos = repos
}

// normalizeRepoRef は repo ref のパスだけを正規化する。island ref と "" はそのまま。
func normalizeRepoRef(ref string) string {
	if kind, v, err := ParseRef(ref); err == nil && kind == RefRepo {
		return RepoRef(NormalizePath(v))
	}
	return ref
}
