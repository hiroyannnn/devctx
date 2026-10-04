package storage

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/hiroyannnn/devctx/model"
	"gopkg.in/yaml.v3"
)

type Storage struct {
	basePath string
}

func New() (*Storage, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	basePath := filepath.Join(home, ".config", "devctx")
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, err
	}
	return &Storage{basePath: basePath}, nil
}

func (s *Storage) contextsPath() string {
	return filepath.Join(s.basePath, "contexts.yaml")
}

func (s *Storage) configPath() string {
	return filepath.Join(s.basePath, "config.yaml")
}

func (s *Storage) insightsPath() string {
	return filepath.Join(s.basePath, "insights.yaml")
}

func (s *Storage) eventsPath() string {
	return filepath.Join(s.basePath, "events.yaml")
}

func (s *Storage) islandsPath() string {
	return filepath.Join(s.basePath, "islands.yaml")
}

func (s *Storage) LoadStore() (*model.Store, error) {
	store := &model.Store{}
	data, err := os.ReadFile(s.contextsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, store); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Storage) SaveStore(store *model.Store) error {
	return s.withFileLock(s.contextsPath(), func() error {
		return s.writeStore(store)
	})
}

// ErrSkipSave を UpdateStore の fn から返すと、保存せずに成功として扱う（filepath.SkipDir と同じ流儀）。
// 高頻度の hook で、変化がないときに contexts.yaml を書き換えないために使う。
var ErrSkipSave = errors.New("skip save")

// UpdateStore atomically loads, updates, and saves contexts with file locking.
// Hooks (register / touch) fire concurrently, e.g. Stop runs touch and roadmap analyze
// at the same time; a plain Load + Save would let the later writer drop the other's change.
func (s *Storage) UpdateStore(fn func(*model.Store) error) error {
	return s.withFileLock(s.contextsPath(), func() error {
		store, err := s.LoadStore()
		if err != nil {
			return err
		}
		if err := fn(store); err != nil {
			if errors.Is(err, ErrSkipSave) {
				return nil
			}
			return err
		}
		return s.writeStore(store)
	})
}

func (s *Storage) writeStore(store *model.Store) error {
	data, err := yaml.Marshal(store)
	if err != nil {
		return err
	}
	return atomicWriteFile(s.contextsPath(), data, 0644)
}

func (s *Storage) LoadConfig() (*model.Config, error) {
	config := defaultConfig()
	data, err := os.ReadFile(s.configPath())
	if err != nil {
		if os.IsNotExist(err) {
			// Write default config
			if err := s.SaveConfig(config); err != nil {
				return nil, err
			}
			return config, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, err
	}
	return config, nil
}

func (s *Storage) SaveConfig(config *model.Config) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(s.configPath(), data, 0644)
}

func (s *Storage) LoadInsights() (*model.InsightStore, error) {
	store := &model.InsightStore{}
	data, err := os.ReadFile(s.insightsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, store); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Storage) SaveInsights(store *model.InsightStore) error {
	return s.withFileLock(s.insightsPath(), func() error {
		data, err := yaml.Marshal(store)
		if err != nil {
			return err
		}
		return atomicWriteFile(s.insightsPath(), data, 0644)
	})
}

// UpdateInsights atomically loads, updates, and saves insights with file locking.
func (s *Storage) UpdateInsights(fn func(*model.InsightStore) error) error {
	return s.withFileLock(s.insightsPath(), func() error {
		store, err := s.LoadInsights()
		if err != nil {
			return err
		}
		if err := fn(store); err != nil {
			return err
		}
		data, err := yaml.Marshal(store)
		if err != nil {
			return err
		}
		return atomicWriteFile(s.insightsPath(), data, 0644)
	})
}

// LoadIslands は islands.yaml を読む。無ければ空（island 機能を使っていない環境で作成しない）。
func (s *Storage) LoadIslands() (*model.IslandStore, error) {
	store := &model.IslandStore{}
	data, err := os.ReadFile(s.islandsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, store); err != nil {
		return nil, err
	}
	return store, nil
}

// UpdateIslands は islands.yaml を 1 つのロック内で読み込み・更新・保存する。
// fn が ErrSkipSave を返すと保存せず成功扱い。
// Why not Load + Save: CLI の連続実行や Web の読み取りと並行しても、後勝ちで他の編集を落とさないため。
func (s *Storage) UpdateIslands(fn func(*model.IslandStore) error) error {
	return s.withFileLock(s.islandsPath(), func() error {
		store, err := s.LoadIslands()
		if err != nil {
			return err
		}
		if err := fn(store); err != nil {
			if errors.Is(err, ErrSkipSave) {
				return nil
			}
			return err
		}
		data, err := yaml.Marshal(store)
		if err != nil {
			return err
		}
		return atomicWriteFile(s.islandsPath(), data, 0644)
	})
}

func (s *Storage) LoadEvents() (*model.EventStore, error) {
	store := &model.EventStore{}
	data, err := os.ReadFile(s.eventsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, store); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Storage) SaveEvents(store *model.EventStore) error {
	return s.withFileLock(s.eventsPath(), func() error {
		data, err := yaml.Marshal(store)
		if err != nil {
			return err
		}
		return atomicWriteFile(s.eventsPath(), data, 0644)
	})
}

// UpdateEvents atomically loads, updates, and saves events with file locking.
func (s *Storage) UpdateEvents(fn func(*model.EventStore) error) error {
	return s.withFileLock(s.eventsPath(), func() error {
		store, err := s.LoadEvents()
		if err != nil {
			return err
		}
		if err := fn(store); err != nil {
			return err
		}
		data, err := yaml.Marshal(store)
		if err != nil {
			return err
		}
		return atomicWriteFile(s.eventsPath(), data, 0644)
	})
}

func (s *Storage) AppendEvent(event model.SessionEvent) error {
	return s.withFileLock(s.eventsPath(), func() error {
		store, err := s.LoadEvents()
		if err != nil {
			return err
		}
		store.Append(event)
		data, err := yaml.Marshal(store)
		if err != nil {
			return err
		}
		return atomicWriteFile(s.eventsPath(), data, 0644)
	})
}

// withFileLock is implemented in lock_unix.go and lock_windows.go.

// atomicWriteFile writes data to a temp file then renames to target path.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func defaultConfig() *model.Config {
	return &model.Config{
		DoneRetentionDays: 1, // Show done items for 1 day by default
		Statuses: []model.StatusConfig{
			{
				Name: model.StatusInProgress,
				Next: []model.Status{model.StatusReview, model.StatusBlocked, model.StatusDone},
			},
			{
				Name: model.StatusReview,
				Next: []model.Status{model.StatusInProgress, model.StatusDone},
				Checklist: []string{
					"/compact",
				},
			},
			{
				Name: model.StatusBlocked,
				Next: []model.Status{model.StatusInProgress},
			},
			{
				Name:    model.StatusDone,
				Next:    []model.Status{},
				Archive: true,
				Checklist: []string{
					"/create-pr",
				},
			},
		},
	}
}
