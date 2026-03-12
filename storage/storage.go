package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

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
	data, err := yaml.Marshal(store)
	if err != nil {
		return err
	}
	return os.WriteFile(s.contextsPath(), data, 0644)
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
	data, err := yaml.Marshal(store)
	if err != nil {
		return err
	}
	return atomicWriteFile(s.insightsPath(), data, 0644)
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
	data, err := yaml.Marshal(store)
	if err != nil {
		return err
	}
	return os.WriteFile(s.eventsPath(), data, 0644)
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

// withFileLock acquires an exclusive file lock, runs fn, and releases the lock.
func (s *Storage) withFileLock(path string, fn func() error) error {
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("cannot create lock file: %w", err)
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("cannot acquire lock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	return fn()
}

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
