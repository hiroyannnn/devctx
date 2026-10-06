package model

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// KindTask は Island.Kind のタスク。"" はテーマ island。
const KindTask = "task"

// Island は手で作るノード。Kind が "" ならテーマ（例: 人事強化）で repo を持たなくてよく、"task" なら 1 件の作業。
// Why: タスクを別の型にせず Island の 1 種にするのは、ref（island:<id>）・改名・削除・付け替えの経路をそのまま使うため。
type Island struct {
	ID     string `yaml:"id" json:"id"`
	Name   string `yaml:"name" json:"name"`
	Parent string `yaml:"parent,omitempty" json:"parent"` // 型付き ref。"" はトップレベル
	Kind   string `yaml:"kind,omitempty" json:"kind,omitempty"`
	Done   bool   `yaml:"done,omitempty" json:"done,omitempty"` // タスクだけが使う
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
	// TaskSeq は採番済みのタスク連番。削除しても戻さない。
	// Why: 消したタスクの id を再利用すると、プロンプトに埋めた marker が別のタスクを指してしまう。
	TaskSeq int `yaml:"task_seq,omitempty" json:"-"`
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

// ParseIslandRef は "island:<id>" だけを受け、id を返す。repo ref など別種別はエラー。
// Why: rename / remove のように island しか取れない操作の入口（CLI と Web）で、種別検査を 1 か所にそろえる。
func ParseIslandRef(ref string) (string, error) {
	kind, v, err := ParseRef(ref)
	if err != nil {
		return "", err
	}
	if kind != RefIsland {
		return "", fmt.Errorf("%s is not an island ref (want island:<id>)", ref)
	}
	return v, nil
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

// isTask は ref が実在するタスクを指すかを返す。
func (s *IslandStore) isTask(ref string) bool {
	kind, v, err := ParseRef(ref)
	if err != nil || kind != RefIsland {
		return false
	}
	is := s.findIsland(v)
	return is != nil && is.Kind == KindTask
}

// errTaskParent は、タスクの下に子を置こうとしたことを表す。
var errTaskParent = errors.New("tasks cannot have children")

// checkParentAllowed は parent の下に子を置けるかを検査する（形式・island の実在・タスクでないこと）。repo は未登録でもよい。
// Why: 次の PR でエージェントセッションがタスクの下に付く。island / repo をタスクの下に許すと、
// 「タスク = 葉」の前提（描画・完了判定）が崩れる。追加・タスク追加・付け替えの全経路でここを通し、
// 読み出し側（ParentOf / Validate）も同じ判定で手編集の親を切る。
func (s *IslandStore) checkParentAllowed(parent string) error {
	kind, v, err := ParseRef(parent)
	if err != nil {
		return err
	}
	if kind != RefIsland {
		return nil
	}
	is := s.findIsland(v)
	if is == nil {
		return fmt.Errorf("island %q not found", v)
	}
	if is.Kind == KindTask {
		return fmt.Errorf("%w (parent %s is a task)", errTaskParent, parent)
	}
	return nil
}

// checkClearable は、refs をトップレベルへ上げてよいか（タスクを含まないか）を検査する。何も変えない。
func (s *IslandStore) checkClearable(refs ...string) error {
	for _, r := range refs {
		if s.isTask(r) {
			return fmt.Errorf("%s is a task; tasks need a parent; move them first", r)
		}
	}
	return nil
}

// clearParents は refs の親を外してトップレベルへ上げる。全体を先に検査し、タスクが 1 つでもあれば何も変えない。
// Why: 親を "" にする経路（Detach / 親なしの島の reparent 削除 / UI に見えない子の切り離し）をここ 1 か所に集め、
// 「タスクは親が要る」を経路ごとに書き直さない。
func (s *IslandStore) clearParents(refs ...string) error {
	if err := s.checkClearable(refs...); err != nil {
		return err
	}
	for _, r := range refs {
		s.setParentUnchecked(r, "")
	}
	return nil
}

var taskIDPattern = regexp.MustCompile(`^t(\d+)$`)

// reservedForTask は id が、採番済みのタスク id（t1 .. t<TaskSeq>）かを返す。
// Why: 削除したタスクの id を後からテーマ島が名乗ると、古いプロンプトの [devctx:task:<id>] がテーマ島を指してしまう。
func (s *IslandStore) reservedForTask(id string) bool {
	m := taskIDPattern.FindStringSubmatch(id)
	if m == nil {
		return false
	}
	n, err := strconv.Atoi(m[1])
	return err == nil && n >= 1 && n <= s.TaskSeq
}

// IDExistsError は明示した island id の衝突（--id）。名前から作る id は衝突時に連番を付けるのでこれにならない。
type IDExistsError struct{ ID string }

func (e *IDExistsError) Error() string {
	return fmt.Sprintf("island id %q already exists; choose another id", e.ID)
}

// AddIsland は island を追加する。id が空なら name の slug（使用済みなら -2, -3 ...）、ASCII が残らなければ island-N。
func (s *IslandStore) AddIsland(name, id, parent string) (Island, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Island{}, fmt.Errorf("island name is empty")
	}
	if parent != "" {
		if err := s.checkParentAllowed(parent); err != nil {
			return Island{}, err
		}
	}
	switch {
	case id != "":
		if !idPattern.MatchString(id) {
			return Island{}, fmt.Errorf("invalid island id %q: use lowercase letters, digits and '-'", id)
		}
		if s.findIsland(id) != nil {
			return Island{}, &IDExistsError{ID: id}
		}
		if s.reservedForTask(id) {
			return Island{}, fmt.Errorf("island id %q is reserved for tasks; choose another id", id)
		}
	case slug(name) != "":
		// 名前から作る id は衝突時に -2, -3 ... を付ける。
		// Why: 改名しても id は残るので、画面に見えない id と衝突する（"API 設計" を "API レビュー" に改名後、また "API" を足す等）。
		// Web には id を選ぶ手段が無く、衝突のたびに足せなくなるのを避ける。
		base := slug(name)
		id = base
		for n := 2; s.findIsland(id) != nil || s.reservedForTask(id); n++ {
			id = base + "-" + strconv.Itoa(n)
		}
	default:
		for n := 1; ; n++ {
			id = "island-" + strconv.Itoa(n)
			if s.findIsland(id) == nil {
				break
			}
		}
	}
	is := Island{ID: id, Name: name, Parent: parent}
	s.Islands = append(s.Islands, is)
	return is, nil
}

// AddTask はタスクを追加する。親（island:<id> または repo:<path>）は必須で、タスクは親なしでは存在しない。
// id は "t" + 連番。TaskSeq を進め、手書きなどで使用済みの id は飛ばす。
// Why not 名前から slug: タスク名は日本語が多く、改名もされる。名前に依存しない id でないと marker が壊れる。
func (s *IslandStore) AddTask(name, parent string) (Island, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Island{}, fmt.Errorf("task name is empty")
	}
	if parent == "" {
		return Island{}, fmt.Errorf("task needs a parent (island:<id> or repo:<path>)")
	}
	if err := s.checkParentAllowed(parent); err != nil {
		return Island{}, err
	}
	n := s.TaskSeq + 1
	for s.findIsland("t"+strconv.Itoa(n)) != nil {
		n++
	}
	s.TaskSeq = n
	is := Island{ID: "t" + strconv.Itoa(n), Name: name, Parent: parent, Kind: KindTask}
	s.Islands = append(s.Islands, is)
	return is, nil
}

// SetTaskDone はタスクの完了状態を設定する。テーマ island には使えない。
func (s *IslandStore) SetTaskDone(id string, done bool) error {
	is := s.findIsland(id)
	if is == nil {
		return fmt.Errorf("island %q not found", id)
	}
	if is.Kind != KindTask {
		return fmt.Errorf("island %q is not a task", id)
	}
	is.Done = done
	return nil
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
	// 生の Parent ではなく解決後の親へ。dangling な親を子へ引き継がない
	return s.removeIsland(id, reparent, s.ParentOf(IslandRef(id)))
}

// removeIsland は、子を newParent へ付け替えて island を消す。
func (s *IslandStore) removeIsland(id string, reparent bool, newParent string) error {
	is := s.findIsland(id)
	if is == nil {
		return fmt.Errorf("island %q not found", id)
	}
	ref := IslandRef(id)
	children := s.Children(ref)
	if len(children) > 0 && !reparent {
		return fmt.Errorf("island %q has %d child(ren) (%s); use --reparent to move them up", id, len(children), strings.Join(children, ", "))
	}
	// 親のない島を reparent で消すと子が最上位へ上がる。タスクの子がいれば clearParents が拒否する（先に別の親へ移してもらう）。
	// テーマ島の子は最上位でもよい。
	if newParent == "" {
		if err := s.clearParents(children...); err != nil {
			return fmt.Errorf("island %q: %w", id, err)
		}
	} else {
		for _, c := range children {
			// 付け替え先は削除対象の親なので存在・循環の検査は不要（木の中で 1 段持ち上げるだけ）
			s.setParentUnchecked(c, newParent)
		}
	}
	for i := range s.Islands {
		if s.Islands[i].ID == id {
			s.Islands = append(s.Islands[:i], s.Islands[i+1:]...)
			break
		}
	}
	return nil
}

// ChildrenChangedError は、呼び出し側が見た子の集合と、ロック内の現在の子が食い違ったことを表す。
type ChildrenChangedError struct{ Current []string }

func (e *ChildrenChangedError) Error() string {
	return fmt.Sprintf("children changed (now: %s)", strings.Join(e.Current, ", "))
}

// RemoveIslandExpecting は、呼び出し側が見た子の集合 expect が現在の子と（集合として）一致するときだけ、
// 子を親へ付け替えて island を消す。子がいなければ expect に関わらず消す。食い違えば *ChildrenChangedError で何も変えない。
// Why: UI が見せていない子（別タブ・CLI の同時編集で増えた子）を、黙って付け替えないため。
// 子と付け替え先は、UI と同じ循環を断ち切った木（Resolved）で決める。生の木で比べると、手編集の輪（A↔B）で
// UI に見えない子との不一致が続き、409 が解消しなくなる。輪を閉じていた辺（UI では切れている子）は先頭へ落とす。
func (s *IslandStore) RemoveIslandExpecting(id string, expect []string) error {
	if !s.HasIsland(id) {
		return s.RemoveIsland(id, false) // not found のエラー
	}
	ref := IslandRef(id)
	resolved := s.Resolved()
	current := resolved.Children(ref)
	want := map[string]bool{}
	for _, r := range expect {
		want[r] = true
	}
	same := len(want) == len(current)
	visible := map[string]bool{}
	for _, r := range current {
		visible[r] = true
		if !want[r] {
			same = false
		}
	}
	if len(current) > 0 && !same {
		return &ChildrenChangedError{Current: current}
	}
	var hidden []string
	for _, c := range s.Children(ref) {
		if !visible[c] {
			hidden = append(hidden, c)
		}
	}
	newParent := resolved.ParentOf(ref)
	// 何かを書き換える前に、失敗しうる検査を済ませる（エラーのときストアを変えない）
	if err := s.checkClearable(hidden...); err != nil {
		return fmt.Errorf("island %q: %w", id, err)
	}
	if newParent == "" {
		if err := s.checkClearable(current...); err != nil {
			return fmt.Errorf("island %q: %w", id, err)
		}
	}
	if err := s.clearParents(hidden...); err != nil {
		return err
	}
	return s.removeIsland(id, len(current) > 0, newParent)
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
	if err := s.checkParentAllowed(parent); err != nil {
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
	// タスクは親の下でだけ意味を持つ（UI のルートへのドロップも無操作）。付け替えは SetParent を使う
	return s.clearParents(child)
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
	// dangling も、手編集でタスクの下に置かれたものも、トップレベルとして扱う
	if s.checkParentAllowed(parent) != nil {
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
		TaskSeq: s.TaskSeq,
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
	var bad, underTask, orphanTasks, badKinds, doneOnNonTask []string
	report := func(ref, parent string) {
		entry := fmt.Sprintf("%s -> %s", ref, parent)
		if errors.Is(s.checkParentAllowed(parent), errTaskParent) {
			underTask = append(underTask, entry)
		} else {
			bad = append(bad, entry)
		}
	}
	for i, is := range s.Islands {
		if is.Parent != "" && r.Islands[i].Parent == "" {
			report(IslandRef(is.ID), is.Parent)
		}
		// 親が "" のタスクは Resolved の前後で変わらないので、上の判定には載らない
		if is.Kind == KindTask && is.Parent == "" {
			orphanTasks = append(orphanTasks, IslandRef(is.ID))
		}
		if is.Kind != "" && is.Kind != KindTask {
			badKinds = append(badKinds, fmt.Sprintf("unknown kind %q: %s", is.Kind, IslandRef(is.ID)))
		}
		if is.Done && is.Kind != KindTask {
			doneOnNonTask = append(doneOnNonTask, IslandRef(is.ID))
		}
	}
	for i, rn := range s.Repos {
		if rn.Parent != "" && r.Repos[i].Parent == "" {
			report(RepoRef(rn.Root), rn.Parent)
		}
	}
	var msgs []string
	if len(orphanTasks) > 0 {
		msgs = append(msgs, fmt.Sprintf("task without parent: %s", strings.Join(orphanTasks, ", ")))
	}
	msgs = append(msgs, badKinds...)
	if len(doneOnNonTask) > 0 {
		msgs = append(msgs, fmt.Sprintf("done on non-task: %s", strings.Join(doneOnNonTask, ", ")))
	}
	if len(bad) > 0 {
		msgs = append(msgs, fmt.Sprintf("invalid parent (dangling or cyclic): %s", strings.Join(bad, "; ")))
	}
	if len(underTask) > 0 {
		msgs = append(msgs, fmt.Sprintf("invalid parent (task): %s", strings.Join(underTask, "; ")))
	}
	if len(msgs) > 0 {
		return fmt.Errorf("%s", strings.Join(msgs, "; "))
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

// HasTask は id が実在するタスク（Kind が task の island）かを返す。
func (s *IslandStore) HasTask(id string) bool {
	is := s.findIsland(id)
	return is != nil && is.Kind == KindTask
}

// FindTask は id のタスクを返す。タスクでなければ nil。
func (s *IslandStore) FindTask(id string) *Island {
	if is := s.findIsland(id); is != nil && is.Kind == KindTask {
		return is
	}
	return nil
}
