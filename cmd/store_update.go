package cmd

import (
	"fmt"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
)

// このファイルの関数は、画面や CLI が先に読んだ store を丸ごと SaveStore しないための入口。
// 読んでから保存するまでの間に hook（touch / register）やダッシュボード API が
// UpdateStore で書いた別フィールドを、古い store で上書きして消さないよう、
// ロック内で読み直した store に「その操作で変えるフィールドだけ」を当てる。

// updateContext はロック内で name の context を引き直して mutate を当て、保存後の store を返す。
// 返した store は呼び出し側の表示用 model の差し替えに使う。mutate は storage.ErrSkipSave で保存を省ける。
func updateContext(s *storage.Storage, name string, mutate func(*model.Context) error) (*model.Store, error) {
	var saved *model.Store
	err := s.UpdateStore(func(store *model.Store) error {
		ctx := store.FindByName(name)
		if ctx == nil {
			return fmt.Errorf("context [%s] not found", name)
		}
		// mutate が storage.ErrSkipSave を返したときも、読み直した store を返す
		saved = store
		return mutate(ctx)
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}

// removeContexts はロック内で names を消し、保存後の store と実際に消した数を返す。
// 1 件も消えなければ書き込まない（読んだ後に別プロセスが先に消した場合）。
func removeContexts(s *storage.Storage, names ...string) (*model.Store, int, error) {
	var current *model.Store
	removed := 0
	err := s.UpdateStore(func(store *model.Store) error {
		current = store
		for _, name := range names {
			if store.Remove(name) {
				removed++
			}
		}
		if removed == 0 {
			return storage.ErrSkipSave
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return current, removed, nil
}

type phaseChange struct {
	Name     string
	Old, New model.Phase
}

// applyPhaseResults はロックの外で取った scan 結果（name → phase）をロック内で当てる。
// gh を叩く scan は数十秒かかりうるので、ロックを握ったまま scan はしない。
// 結果は store の並び順で返す（refresh の表示順を変えないため）。
// phaseScan はロック外で取った scan 結果と、そのとき見た取得元。
// scan 中に register が同名 context を別 worktree / branch で登録し直したら、結果は当てない。
type phaseScan struct {
	Phase            model.Phase
	Worktree, Branch string
}

func applyPhaseResults(s *storage.Storage, phases map[string]phaseScan, checkedAt time.Time) ([]phaseChange, error) {
	var changes []phaseChange
	err := s.UpdateStore(func(store *model.Store) error {
		for i := range store.Contexts {
			ctx := &store.Contexts[i]
			scan, ok := phases[ctx.Name]
			if !ok {
				continue
			}
			if ctx.Worktree != scan.Worktree || ctx.Branch != scan.Branch {
				continue
			}
			phase := scan.Phase
			old := ctx.Phase
			// Scanner.RefreshPhase と同じ規則: idle は「判定できなかった」とみなし、既知の phase を消さない
			if phase != model.PhaseIdle || ctx.Phase == "" {
				ctx.Phase = phase
			}
			ctx.PhaseCheckedAt = checkedAt
			changes = append(changes, phaseChange{Name: ctx.Name, Old: old, New: ctx.Phase})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changes, nil
}

// applyMove は status 移動と、対話で答えた checklist だけをロック内で当てる。
func applyMove(s *storage.Storage, name string, target model.Status, checklist map[string]bool, now time.Time) error {
	_, err := updateContext(s, name, func(ctx *model.Context) error {
		if len(checklist) > 0 && ctx.Checklist == nil {
			ctx.Checklist = make(map[string]bool, len(checklist))
		}
		for item, done := range checklist {
			ctx.Checklist[item] = done
		}
		ctx.Status = target
		ctx.LastSeen = now
		return nil
	})
	return err
}

// linkUpdate は GitHub / transcript から取った値と、そのとき見た取得元。空のフィールドは触らない。
// PR / Issue は Worktree と Branch（branch 名は main など repo をまたいで重なる）、SessionName は TranscriptPath が最新の context と一致するときだけ当てる
// （問い合わせ中に register が別セッションで登録し直した場合に、古い値を混ぜないため）。
type linkUpdate struct {
	PRURL, IssueURL, SessionName     string
	Worktree, Branch, TranscriptPath string
}

// applyLinkUpdates は gh の問い合わせ後に、取れた値だけをロック内で当てる。
// 対象が読んだ後に消えていたら飛ばす（復活させない）。
func applyLinkUpdates(s *storage.Storage, updates map[string]linkUpdate) error {
	return s.UpdateStore(func(store *model.Store) error {
		changed := false
		for name, u := range updates {
			ctx := store.FindByName(name)
			if ctx == nil {
				continue
			}
			if ctx.Worktree == u.Worktree && ctx.Branch == u.Branch {
				if u.PRURL != "" {
					ctx.PRURL = u.PRURL
					changed = true
				}
				if u.IssueURL != "" {
					ctx.IssueURL = u.IssueURL
					changed = true
				}
			}
			// 補完済みなら上書きしない（needsSessionNameRefresh と同じく空のときだけ埋める）
			if u.SessionName != "" && ctx.SessionName == "" && ctx.TranscriptPath == u.TranscriptPath {
				ctx.SessionName = u.SessionName
				changed = true
			}
		}
		if !changed {
			return storage.ErrSkipSave
		}
		return nil
	})
}
