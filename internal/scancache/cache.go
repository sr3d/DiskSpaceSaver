// Package scancache persists a finished scan so reopening a volume is instant.
package scancache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/sr3d/diskspacesaver/internal/prefs"
	"github.com/sr3d/diskspacesaver/internal/tree"
)

// Meta describes a saved scan without loading it.
type Meta struct {
	Key     string    `json:"key"`
	Root    string    `json:"root"`
	SavedAt time.Time `json:"savedAt"`
	Nodes   int       `json:"nodes"`
	Bytes   int64     `json:"bytes"`
}

// Cache is a directory of saved scans.
type Cache struct{ dir string }

// Open returns the cache under Application Support. A cache that cannot be
// created is not fatal: the app simply always rescans.
func Open() *Cache {
	base, err := prefs.SupportDir()
	if err != nil {
		return &Cache{}
	}
	dir := filepath.Join(base, "scans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &Cache{}
	}
	return &Cache{dir: dir}
}

// PathKey derives a stable cache key for an arbitrary folder. Volumes are
// keyed by UUID instead, which survives renaming and remounting.
func PathKey(path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return "path-" + hex.EncodeToString(sum[:8])
}

func safeKey(key string) string {
	k := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, key)
	if len(k) > 96 {
		k = k[:96]
	}
	return k
}

func (c *Cache) file(key string) string     { return filepath.Join(c.dir, safeKey(key)+".bin") }
func (c *Cache) metaFile(key string) string { return filepath.Join(c.dir, safeKey(key)+".json") }

// Save writes a finished scan. Writing is atomic so an interrupted save cannot
// leave a half-written cache that fails to load next launch.
func (c *Cache) Save(key, root string, t *tree.Tree) error {
	if c.dir == "" {
		return errors.New("no cache directory")
	}
	rootNode, ok := t.Snapshot(t.Root)
	if !ok {
		return errors.New("empty tree")
	}

	tmp := c.file(key) + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc, err := zstd.NewWriter(f, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := t.Encode(enc); err != nil {
		enc.Close()
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := enc.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, c.file(key)); err != nil {
		os.Remove(tmp)
		return err
	}

	meta := Meta{Key: key, Root: root, SavedAt: time.Now(), Nodes: t.Count(), Bytes: rootNode.Alloc}
	b, _ := json.MarshalIndent(meta, "", "  ")
	return os.WriteFile(c.metaFile(key), b, 0o644)
}

// Stat reads a cache entry's metadata, or nil when there is none.
func (c *Cache) Stat(key string) (*Meta, error) {
	if c.dir == "" {
		return nil, nil
	}
	b, err := os.ReadFile(c.metaFile(key))
	if err != nil {
		return nil, nil
	}
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, nil
	}
	if _, err := os.Stat(c.file(key)); err != nil {
		return nil, nil
	}
	return &m, nil
}

// Load reconstructs a saved scan.
func (c *Cache) Load(key string) (*tree.Tree, time.Time, error) {
	if c.dir == "" {
		return nil, time.Time{}, errors.New("no cache directory")
	}
	meta, _ := c.Stat(key)
	f, err := os.Open(c.file(key))
	if err != nil {
		return nil, time.Time{}, err
	}
	defer f.Close()
	dec, err := zstd.NewReader(f)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer dec.Close()

	t, err := tree.Decode(dec)
	if err != nil {
		// A cache we cannot read is worse than no cache: drop it so the next
		// scan starts clean instead of failing the same way every launch.
		c.Delete(key)
		return nil, time.Time{}, fmt.Errorf("saved scan could not be read: %w", err)
	}
	var savedAt time.Time
	if meta != nil {
		savedAt = meta.SavedAt
	}
	return t, savedAt, nil
}

// Delete removes one cache entry.
func (c *Cache) Delete(key string) {
	if c.dir == "" {
		return
	}
	os.Remove(c.file(key))
	os.Remove(c.metaFile(key))
}

// List returns every saved scan, newest first.
func (c *Cache) List() []Meta {
	if c.dir == "" {
		return nil
	}
	ents, err := os.ReadDir(c.dir)
	if err != nil {
		return nil
	}
	var out []Meta
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(c.dir, e.Name()))
		if err != nil {
			continue
		}
		var m Meta
		if json.Unmarshal(b, &m) == nil {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SavedAt.After(out[j].SavedAt) })
	return out
}

// Clear empties the cache, for the Preferences button.
func (c *Cache) Clear() {
	for _, m := range c.List() {
		c.Delete(m.Key)
	}
}

// TotalBytes is how much disk the cache itself occupies.
func (c *Cache) TotalBytes() int64 {
	if c.dir == "" {
		return 0
	}
	var total int64
	ents, _ := os.ReadDir(c.dir)
	for _, e := range ents {
		if info, err := e.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

// writeFile is used by the tests to corrupt an entry deliberately.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
