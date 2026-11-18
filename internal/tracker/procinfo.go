package tracker

import (
	"sync"
	"time"
)

type procInfo struct {
	Name  string // process (executable) name
	Owner string // user name, or numeric id if it cannot be resolved
}

// procCache avoids re-reading process details on every title change of the
// same window. Entries expire so a recycled PID is eventually picked up.
type procCache struct {
	mu      sync.Mutex
	entries map[int]procEntry
	ttl     time.Duration
}

type procEntry struct {
	info    procInfo
	expires time.Time
}

func newProcCache() *procCache {
	return &procCache{entries: map[int]procEntry{}, ttl: 30 * time.Second}
}

func (c *procCache) lookup(pid int) procInfo {
	now := time.Now()
	c.mu.Lock()
	if e, ok := c.entries[pid]; ok && now.Before(e.expires) {
		c.mu.Unlock()
		return e.info
	}
	c.mu.Unlock()

	info, err := processInfo(pid)
	if err != nil {
		return procInfo{}
	}
	c.mu.Lock()
	if len(c.entries) > 512 {
		c.entries = map[int]procEntry{}
	}
	c.entries[pid] = procEntry{info: info, expires: now.Add(c.ttl)}
	c.mu.Unlock()
	return info
}
