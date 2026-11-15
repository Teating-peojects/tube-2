package metrics

import (
	"sync"
	"testing"
)

func TestCounters(t *testing.T) {
	c := New()
	if c.Get("x") != 0 {
		t.Fatal("unknown counter must read 0")
	}
	c.Add("b", 2)
	c.Add("a", 1)
	c.Add("b", 3)
	c.Add("ignored", 0)
	c.Add("ignored", -5)
	values, names := c.Snapshot()
	if values["a"] != 1 || values["b"] != 5 || len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("snapshot %v %v", values, names)
	}
}

func TestCountersAreSafeForConcurrentUse(t *testing.T) {
	c := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Add("n", 1)
			}
		}()
	}
	wg.Wait()
	if got := c.Get("n"); got != 5000 {
		t.Fatalf("lost updates: %d", got)
	}
}
