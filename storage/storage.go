package storage

import (
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
	return os.WriteFile(s.insightsPath(), data, 0644)
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
