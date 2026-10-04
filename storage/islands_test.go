package storage

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

func TestLoadIslandsReturnsEmptyWhenMissing(t *testing.T) {
	s := &Storage{basePath: t.TempDir()}
	got, err := s.LoadIslands()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Islands) != 0 || len(got.Repos) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestUpdateIslandsRoundTrip(t *testing.T) {
	s := &Storage{basePath: t.TempDir()}
	err := s.UpdateIslands(func(st *model.IslandStore) error {
		if _, err := st.AddIsland("人事強化", "hr", ""); err != nil {
			return err
		}
		return st.SetParent("repo:/r/a", "island:hr")
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadIslands()
	if err != nil {
		t.Fatal(err)
	}
	want := &model.IslandStore{
		Islands: []model.Island{{ID: "hr", Name: "人事強化"}},
		Repos:   []model.RepoNode{{Root: "/r/a", Parent: "island:hr"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestUpdateIslandsSerializesConcurrentUpdates(t *testing.T) {
	s := &Storage{basePath: t.TempDir()}
	const writers = 20
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- s.UpdateIslands(func(st *model.IslandStore) error {
				_, err := st.AddIsland(fmt.Sprintf("n%d", i), fmt.Sprintf("i%d", i), "")
				return err
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.LoadIslands()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Islands) != writers {
		t.Fatalf("islands = %d, want %d (updates were lost)", len(got.Islands), writers)
	}
}

func TestUpdateIslandsDoesNotSaveOnError(t *testing.T) {
	s := &Storage{basePath: t.TempDir()}
	if err := s.UpdateIslands(func(st *model.IslandStore) error {
		_, err := st.AddIsland("keep", "keep", "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("abort")
	err := s.UpdateIslands(func(st *model.IslandStore) error {
		st.Islands = nil
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v", err)
	}
	got, _ := s.LoadIslands()
	if len(got.Islands) != 1 {
		t.Errorf("failed update was persisted: %+v", got)
	}
}

func TestUpdateIslandsSkipSave(t *testing.T) {
	s := &Storage{basePath: t.TempDir()}
	if err := s.UpdateIslands(func(st *model.IslandStore) error { return ErrSkipSave }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.islandsPath()); !os.IsNotExist(err) {
		t.Errorf("islands.yaml should not be created: %v", err)
	}
}

func TestLoadIslandsReturnsErrorOnInvalidYAML(t *testing.T) {
	s := &Storage{basePath: t.TempDir()}
	if err := os.WriteFile(s.islandsPath(), []byte("islands: [unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadIslands(); err == nil {
		t.Error("want error")
	}
}
