package model

import (
	"regexp"
	"strings"
	"time"
)

// TaskMarkerPrefix は、タスクをプロンプトとしてコピーするときに最終行へ添える marker の接頭辞。
// セッションの最初のプロンプトからこの marker を拾い、タスクの下へ自動で付ける（ParseTaskMarker）。
// Why: 形式をここで先に固定するのは、UI（JS 側に同じ書式を持つ）と後段の検出が食い違わないようにするため。
const TaskMarkerPrefix = "[devctx:task:"

// TaskMarker は id のタスクを指す marker（例: "[devctx:task:t3]"）を返す。
func TaskMarker(id string) string { return TaskMarkerPrefix + id + "]" }

// TaskLinkSourceMarker / TaskLinkSourceManual は Context.TaskLinkSource の値。
const (
	TaskLinkSourceMarker = "marker"
	TaskLinkSourceManual = "manual"
)

var (
	taskMarkerIDPattern = regexp.MustCompile(`^t[1-9]\d*$`)
	// pastedContentTag は、貼り付けたプロンプトの前後に Claude Code が付けるタグ行。marker の検出では無いものとして扱う
	pastedContentTag = regexp.MustCompile(`^</?pasted_content( id="[^"]*")?>$`)
)

// ParseTaskMarker はプロンプトの最終行（pasted_content のタグ行と空行を除く）が marker そのものなら、そのタスク id を返す。
// Why 最終行だけ: 本文中の引用や説明に marker が出てきたときに、意図せずタスクへ付けないため。
// Why not 部分一致: 「marker をコピーして貼る」操作で付くのは常に最終行で、それ以外は偶然の混入とみなせる。
// プロンプト本文は保存せず、ここでメモリ上だけで解釈する。
func ParseTaskMarker(prompt string) (string, bool) {
	lines := strings.Split(prompt, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || pastedContentTag.MatchString(line) {
			continue
		}
		if !strings.HasPrefix(line, TaskMarkerPrefix) || !strings.HasSuffix(line, "]") {
			return "", false
		}
		id := strings.TrimSuffix(strings.TrimPrefix(line, TaskMarkerPrefix), "]")
		if !taskMarkerIDPattern.MatchString(id) {
			return "", false
		}
		return id, true
	}
	return "", false
}

// ApplyTaskMarker は、sessionID のプロンプトに付いていた marker を ctx に適用し、変化があれば true を返す。
// 1 セッションにつき最初の marker だけが効く。ただし async hook は順序が入れ替わりうるので、
// 同じセッションでも より早い時刻のイベントが後から届いたら、それを最初の marker として採る。
// 前のセッション（Claude は同じ worktree の context を使い回す）で付いたリンクは、新セッションの最初の marker で付け替わる。
// タスクの完了状態やコミットメントには触れない（context の紐付けだけを書き換える）。
func ApplyTaskMarker(ctx *Context, sessionID, taskID string, at time.Time) bool {
	if sessionID == "" || sessionID != ctx.SessionID {
		return false
	}
	ref := IslandRef(taskID)
	if ctx.TaskLinkSession == sessionID {
		if !at.Before(ctx.TaskLinkAt) {
			return false
		}
		if ctx.TaskRef == ref {
			// 同じタスクでも時刻は早い方へ引き戻す。引き戻さないと、後から届く「A より遅く C より早い」別タスクの marker が最初の marker として勝ってしまう
			ctx.TaskLinkAt = at
			return false
		}
		ctx.TaskRef, ctx.TaskLinkSource, ctx.TaskLinkAt = ref, TaskLinkSourceMarker, at
		return true
	}
	ctx.TaskRef, ctx.TaskLinkSession, ctx.TaskLinkSource, ctx.TaskLinkAt = ref, sessionID, TaskLinkSourceMarker, at
	return true
}

// LinkTask は手動で ctx をタスクに付ける。現在のセッションの最初の marker 扱いなので、以降の marker では動かない。
func LinkTask(ctx *Context, taskRef string, at time.Time) {
	ctx.TaskRef, ctx.TaskLinkSession, ctx.TaskLinkSource, ctx.TaskLinkAt = taskRef, ctx.SessionID, TaskLinkSourceManual, at
}

// UnlinkTask はタスクへの紐付けを外す。
func UnlinkTask(ctx *Context) {
	ctx.TaskRef, ctx.TaskLinkSession, ctx.TaskLinkSource, ctx.TaskLinkAt = "", "", "", time.Time{}
}
