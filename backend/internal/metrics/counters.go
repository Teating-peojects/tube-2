// Package metrics holds a tiny in-process counter registry. There is no scrape endpoint yet; the janitor logs a
// snapshot periodically (see cmd/api). It exists so services can count events without depending on a metrics stack.
package metrics

import (
	"sort"
	"sync"
	"sync/atomic"
)

// Counters is safe for concurrent use. The zero value is not usable, call New.
type Counters struct {
	mu sync.RWMutex
	m  map[string]*atomic.Int64
}

func New() *Counters { return &Counters{m: map[string]*atomic.Int64{}} }

// Add increases the named counter by n (negative values are ignored).
func (c *Counters) Add(name string, n int64) {
	if n <= 0 {
		return
	}
	c.mu.RLock()
	v, ok := c.m[name]
	c.mu.RUnlock()
	if !ok {
		c.mu.Lock()
		if v, ok = c.m[name]; !ok {
			v = new(atomic.Int64)
			c.m[name] = v
		}
		c.mu.Unlock()
	}
	v.Add(n)
}

// Get returns the current value (0 for an unknown counter).
func (c *Counters) Get(name string) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if v, ok := c.m[name]; ok {
		return v.Load()
	}
	return 0
}

// Snapshot returns every counter that has been incremented, plus their names in sorted order.
func (c *Counters) Snapshot() (map[string]int64, []string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]int64, len(c.m))
	names := make([]string, 0, len(c.m))
	for k, v := range c.m {
		out[k] = v.Load()
		names = append(names, k)
	}
	sort.Strings(names)
	return out, names
}
