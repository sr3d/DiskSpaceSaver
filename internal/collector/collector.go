// Package collector is the drop tray: a basket of items gathered across every
// open scan, showing the total that would be reclaimed, deletable in one go.
package collector

import (
	"fmt"
	"runtime"
	"sync"
)

// Item is one collected entry. Path is captured at add time so the tray still
// knows what it holds after its scan tab is closed.
type Item struct {
	ScanID   string `json:"scanID"`
	NodeID   uint32 `json:"nodeID"`
	Path     string `json:"path"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Kind     uint8  `json:"kind"`
	ScanRoot string `json:"scanRoot"`
	AsAdmin  bool   `json:"asAdmin"`
}

func key(scanID string, nodeID uint32) string {
	return fmt.Sprintf("%s:%d", scanID, nodeID)
}

// State is the whole tray, as the UI renders it.
type State struct {
	Items []Item `json:"items"`
	Total int64  `json:"total"`
	Count int    `json:"count"`
}

// Collector is concurrency-safe.
type Collector struct {
	mu    sync.RWMutex
	items []Item
	seen  map[string]bool
}

// New creates an empty tray.
func New() *Collector { return &Collector{seen: map[string]bool{}} }

// Add puts items in the tray, ignoring ones already there.
//
// An item whose ancestor is already collected is skipped: deleting the parent
// removes the child anyway, and counting both would overstate what the user
// gets back.
func (c *Collector) Add(items ...Item) State {
	c.mu.Lock()
	for _, it := range items {
		if it.Path == "" || c.seen[key(it.ScanID, it.NodeID)] {
			continue
		}
		if c.coveredLocked(it) {
			continue
		}
		c.dropDescendantsLocked(it)
		c.seen[key(it.ScanID, it.NodeID)] = true
		c.items = append(c.items, it)
	}
	st := c.stateLocked()
	c.mu.Unlock()
	return st
}

// coveredLocked reports whether an ancestor of it is already collected.
func (c *Collector) coveredLocked(it Item) bool {
	for _, ex := range c.items {
		if ex.ScanID == it.ScanID && isAncestor(ex.Path, it.Path) {
			return true
		}
	}
	return false
}

// dropDescendantsLocked removes entries that the new item subsumes.
func (c *Collector) dropDescendantsLocked(it Item) {
	kept := c.items[:0]
	for _, ex := range c.items {
		if ex.ScanID == it.ScanID && isAncestor(it.Path, ex.Path) {
			delete(c.seen, key(ex.ScanID, ex.NodeID))
			continue
		}
		kept = append(kept, ex)
	}
	c.items = kept
}

// isAncestor reports whether child sits inside parent. The separator check is
// what stops "/v/about.txt" being treated as a child of "/v/a" — without it the
// tray would silently drop a sibling whose name merely shares a prefix.
func isAncestor(parent, child string) bool {
	if parent == child || parent == "" {
		return false
	}
	if len(child) <= len(parent) {
		return false
	}
	if child[:len(parent)] != parent {
		return false
	}
	// A volume root ("/", or "C:\\") already ends in its separator, so there is
	// no second one to find at the boundary; requiring one would say nothing on
	// the disk is inside it, and collecting a whole volume would stop
	// subsuming the items already taken from it.
	if isSeparator(parent[len(parent)-1]) {
		return true
	}
	return isSeparator(child[len(parent)])
}

// isSeparator accepts both separators on Windows, where a path may legitimately
// arrive with either.
func isSeparator(c byte) bool {
	if c == '/' {
		return true
	}
	return runtime.GOOS == "windows" && c == '\\'
}

// Remove takes one item out of the tray.
func (c *Collector) Remove(scanID string, nodeID uint32) State {
	c.mu.Lock()
	k := key(scanID, nodeID)
	if c.seen[k] {
		delete(c.seen, k)
		kept := c.items[:0]
		for _, it := range c.items {
			if it.ScanID == scanID && it.NodeID == nodeID {
				continue
			}
			kept = append(kept, it)
		}
		c.items = kept
	}
	st := c.stateLocked()
	c.mu.Unlock()
	return st
}

// RemoveScan drops everything belonging to a closed scan tab.
func (c *Collector) RemoveScan(scanID string) State {
	c.mu.Lock()
	kept := c.items[:0]
	for _, it := range c.items {
		if it.ScanID == scanID {
			delete(c.seen, key(it.ScanID, it.NodeID))
			continue
		}
		kept = append(kept, it)
	}
	c.items = kept
	st := c.stateLocked()
	c.mu.Unlock()
	return st
}

// Clear empties the tray.
func (c *Collector) Clear() State {
	c.mu.Lock()
	c.items = nil
	c.seen = map[string]bool{}
	st := c.stateLocked()
	c.mu.Unlock()
	return st
}

// List returns the current contents.
func (c *Collector) List() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stateLocked()
}

// Has reports whether an item is in the tray, for the UI's checkmarks.
func (c *Collector) Has(scanID string, nodeID uint32) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.seen[key(scanID, nodeID)]
}

func (c *Collector) stateLocked() State {
	out := State{Items: make([]Item, len(c.items)), Count: len(c.items)}
	copy(out.Items, c.items)
	for _, it := range c.items {
		out.Total += it.Size
	}
	return out
}
