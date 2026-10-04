package model

import "path/filepath"

// NormalizePath は比較・グルーピング用にパスを正規化する。symlink 解決に失敗（不在等）したら Clean のみ。
// Why not 失敗をエラーで返す: 呼び出し側（表示・照合）は不在パスでも動き続けたい。
func NormalizePath(p string) string {
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// RepoKey は context が属する repo の識別キーを返す（RepoRoot、なければ Worktree。どちらも空なら ""）。
// Why: symlink 経由と実パスで同じ repo が別ノードに割れるのを防ぎ、islands.yaml の repo 参照と突き合わせる共通キーにする。
func RepoKey(ctx Context) string {
	if ctx.RepoRoot != "" {
		return NormalizePath(ctx.RepoRoot)
	}
	return NormalizePath(ctx.Worktree)
}
