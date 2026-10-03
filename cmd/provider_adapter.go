package cmd

import "github.com/hiroyannnn/devctx/model"

// ProviderAdapter はエージェント固有の処理（セッション探索と再開コマンド）だけを担う。
// 登録済み判定・保存・worktree への cd は provider 共通の層に残し、
// adapter ごとに重複実装されないようにする。
type ProviderAdapter interface {
	Provider() model.Provider
	Discover(store *model.Store) ([]DiscoveredSession, error)
	// AgentCommand は worktree 内で実行するエージェント再開コマンドを返す。
	AgentCommand(ctx model.Context) (string, error)
}

// providerAdapters は discover の表示順でもある。manual は探索も再開もないので登録しない。
func providerAdapters() []ProviderAdapter {
	return []ProviderAdapter{claudeAdapter{}, newCodexAdapter()}
}

// tracksPerSession は provider がセッション単位で context を持つかを返す。
// Why: Codex は discover がセッション単位で取り込む。Claude は既存 UX のとおり worktree ごとに 1 context を保つ。
func tracksPerSession(p model.Provider) bool { return p == model.ProviderCodex }

func adapterFor(p model.Provider) (ProviderAdapter, bool) {
	for _, a := range providerAdapters() {
		if a.Provider() == p {
			return a, true
		}
	}
	return nil, false
}
