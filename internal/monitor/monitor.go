// Package monitor is the watchlist: folders gathered across every open scan
// and kept in view, so their sizes can be compared now and watched afterwards.
//
// It is deliberately not the same idea as the ticked selection. Ticks are a
// working set being assembled for one action and emptied straight after;
// watched folders are meant to sit there across scans, which is why an entry
// records what it weighed when it was first added.
package monitor

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

// Item is one watched entry. Path is captured at add time so the list still
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
	// Base is what the item weighed when it was first watched, and AddedAt is
	// when that was. Together they are the point of watching something rather
	// than merely listing it: for a folder you suspect of growing, the number
	// that matters is not its size but the change since you looked.
	//
	// Base is set here, not by the caller, so it cannot drift from the size
	// recorded beside it; re-adding something already watched leaves both
	// alone, because resetting them would quietly discard the history.
	Base    int64 `json:"base"`
	AddedAt int64 `json:"addedAt"`
	// Gone marks a path that was not there when last re-measured. Kept on the
	// list rather than dropped: a watched folder disappearing is a result, and
	// one that silently vanished from the panel would look like a bug.
	Gone bool `json:"gone"`
	// MeasuredAt is when Size was last confirmed against the disk, zero if it
	// has not been since it was added.
	MeasuredAt int64 `json:"measuredAt"`
}

func key(scanID string, nodeID uint32) string {
	return fmt.Sprintf("%s:%d", scanID, nodeID)
}

// State is the whole watchlist, as the UI renders it.
type State struct {
	Items []Item `json:"items"`
	Total int64  `json:"total"`
	Count int    `json:"count"`
	// BaseTotal is what these items weighed between them when each was first
	// watched, so the UI can show the change without summing it itself.
	BaseTotal int64 `json:"baseTotal"`
}

// Monitor is concurrency-safe.
type Monitor struct {
	mu    sync.RWMutex
	items []Item
	seen  map[string]bool
}

// New creates an empty watchlist.
func New() *Monitor { return &Monitor{seen: map[string]bool{}} }

// Add starts watching items, ignoring ones already watched.
//
// An item whose ancestor is already watched is skipped: its bytes are inside
// that ancestor, so listing both would count them twice in the total and
// report the same growth from two places.
func (c *Monitor) Add(items ...Item) State {
	now := time.Now().Unix()
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
		it.Base, it.AddedAt = it.Size, now
		c.items = append(c.items, it)
	}
	st := c.stateLocked()
	c.mu.Unlock()
	return st
}

// coveredLocked reports whether an ancestor of it is already watched.
func (c *Monitor) coveredLocked(it Item) bool {
	for _, ex := range c.items {
		if ex.ScanID == it.ScanID && isAncestor(ex.Path, it.Path) {
			return true
		}
	}
	return false
}

// dropDescendantsLocked removes entries that the new item subsumes.
func (c *Monitor) dropDescendantsLocked(it Item) {
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
// list would silently drop a sibling whose name merely shares a prefix.
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
	// the disk is inside it, and watching a whole volume would stop
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

// Remove stops watching one item.
func (c *Monitor) Remove(scanID string, nodeID uint32) State {
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
func (c *Monitor) RemoveScan(scanID string) State {
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

// Clear stops watching everything.
func (c *Monitor) Clear() State {
	c.mu.Lock()
	c.items = nil
	c.seen = map[string]bool{}
	st := c.stateLocked()
	c.mu.Unlock()
	return st
}

// List returns the current contents.
func (c *Monitor) List() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stateLocked()
}

// Update records a fresh measurement of one watched item. Base and AddedAt
// are left alone — they are the thing being measured against.
func (c *Monitor) Update(scanID string, nodeID uint32, size int64, gone bool) State {
	c.mu.Lock()
	for i := range c.items {
		if c.items[i].ScanID != scanID || c.items[i].NodeID != nodeID {
			continue
		}
		if !gone {
			c.items[i].Size = size
		}
		c.items[i].Gone = gone
		c.items[i].MeasuredAt = time.Now().Unix()
		break
	}
	st := c.stateLocked()
	c.mu.Unlock()
	return st
}

// Has reports whether an item is watched, for the UI's marks.
func (c *Monitor) Has(scanID string, nodeID uint32) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.seen[key(scanID, nodeID)]
}

func (c *Monitor) stateLocked() State {
	out := State{Items: make([]Item, len(c.items)), Count: len(c.items)}
	copy(out.Items, c.items)
	for _, it := range c.items {
		out.Total += it.Size
		out.BaseTotal += it.Base
	}
	return out
}
