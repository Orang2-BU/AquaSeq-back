package cache

import (
	"sync"
	"testing"
	"time"
)

func TestCacheSetGetDelete(t *testing.T) {
	c := New()

	c.Set("foo", "bar", 1*time.Minute)
	val, ok := c.Get("foo")
	if !ok || val != "bar" {
		t.Fatalf("expected bar, got %v (ok: %v)", val, ok)
	}

	c.Delete("foo")
	_, ok = c.Get("foo")
	if ok {
		t.Fatalf("expected key to be deleted")
	}
}

func TestCacheTTLExpiration(t *testing.T) {
	c := New()

	c.Set("temp", 123, 10*time.Millisecond)
	val, ok := c.Get("temp")
	if !ok || val != 123 {
		t.Fatalf("expected 123, got %v", val)
	}

	time.Sleep(20 * time.Millisecond)
	_, ok = c.Get("temp")
	if ok {
		t.Fatalf("expected key to be expired")
	}
}

func TestCacheConcurrentAccess(t *testing.T) {
	c := New()
	concurrency := 1000
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				c.Set("key", idx, 100*time.Millisecond)
			} else {
				_, _ = c.Get("key")
			}
		}(i)
	}

	wg.Wait()
}
