package server_test

import (
	"os"
	"path/filepath"
	"testing"

	"fas/pkg/persistence"
	"fas/pkg/server"
	"fas/pkg/store"
)

func TestSaveAndRestoreSnapshot(t *testing.T) {
	dir := t.TempDir()
	rdb := filepath.Join(dir, "test.rdb")

	cfg := server.Config{
		Host:        "127.0.0.1",
		Port:        0,
		RDBPath:     rdb,
		FsyncPolicy: 0,
		MaxMemory:   1024 * 1024,
	}
	s := server.NewServer(cfg)
	s.Store().Set("k", "v", 0)

	if err := s.SaveSnapshot(); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	if _, err := os.Stat(rdb); err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}

	entries, err := persistence.LoadSnapshot(rdb)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	restoreStore := store.New(cfg.MaxMemory, store.EvictionAllKeysRandom)
	restoreStore.RestoreSnapshot(entries)
	got, err := restoreStore.Get("k")
	if err != nil || got != "v" {
		t.Fatalf("restore mismatch, got %q err=%v", got, err)
	}
}
