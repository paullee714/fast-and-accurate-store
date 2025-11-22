package persistence

import (
	"encoding/gob"
	"os"
	"path/filepath"

	"fas/pkg/store"
)

// SaveSnapshot writes snapshot entries to the given path atomically (temp + rename).
func SaveSnapshot(path string, entries []store.SnapshotEntry) error {
	dir := filepath.Dir(path)
	tmp := filepath.Join(dir, ".tmp-"+filepath.Base(path))

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	enc := gob.NewEncoder(f)
	if err := enc.Encode(entries); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadSnapshot reads snapshot entries from path.
func LoadSnapshot(path string) ([]store.SnapshotEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []store.SnapshotEntry
	dec := gob.NewDecoder(f)
	if err := dec.Decode(&entries); err != nil {
		return nil, err
	}
	return entries, nil
}
