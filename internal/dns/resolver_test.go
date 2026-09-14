package dns

import (
	"testing"
)

func TestUpstreamKey(t *testing.T) {
	u1 := Upstream{Addr: "8.8.8.8:53", Mode: "udp"}
	u2 := Upstream{Addr: "8.8.8.8:53", Mode: "tcp"}
	u3 := Upstream{Addr: "8.8.8.8:53", Mode: "udp"}

	if upstreamKey(u1) != upstreamKey(u3) {
		t.Errorf("same mode+addr should have same key, got %q vs %q", upstreamKey(u1), upstreamKey(u3))
	}
	if upstreamKey(u1) == upstreamKey(u2) {
		t.Errorf("different modes should differ, both %q", upstreamKey(u1))
	}
}

func TestMarkFailEntersCooldown(t *testing.T) {
	r := NewResolver([]string{"8.8.8.8:53"})
	key := "udp|8.8.8.8:53"

	for i := 0; i < cooldownAfter; i++ {
		r.markFail(key)
	}

	r.mu.RLock()
	h := r.health[key]
	r.mu.RUnlock()

	if h == nil {
		t.Fatal("expected health entry created")
	}
	if h.failures != 0 {
		t.Errorf("expected failures reset after entering cooldown, got %d", h.failures)
	}
	if h.cooldownUntil.IsZero() {
		t.Error("expected cooldownUntil set after enough failures")
	}
}

func TestMarkSuccessClearsHealth(t *testing.T) {
	r := NewResolver([]string{"8.8.8.8:53"})
	key := "udp|8.8.8.8:53"

	r.markFail(key)
	r.markSuccess(key)

	r.mu.RLock()
	h := r.health[key]
	r.mu.RUnlock()

	if h == nil {
		t.Fatal("expected health entry present after fail then success")
	}
	if h.failures != 0 {
		t.Errorf("expected failures reset to 0, got %d", h.failures)
	}
	if !h.cooldownUntil.IsZero() {
		t.Error("expected cooldownUntil cleared after success")
	}
}

func TestNewResolver(t *testing.T) {
	upstreams := []string{"8.8.8.8:53", "1.1.1.1:53"}
	r := NewResolver(upstreams)
	if r == nil {
		t.Fatal("NewResolver returned nil")
	}
}

func TestSetUpstreams(t *testing.T) {
	r := NewResolver([]string{"8.8.8.8:53"})
	newUpstreams := []string{"1.1.1.1:53", "223.5.5.5:53"}
	r.SetUpstreams(newUpstreams)

	if len(r.upstreams) != 2 {
		t.Errorf("expected 2 upstreams, got %d", len(r.upstreams))
	}
}

func TestClearCache(t *testing.T) {
	r := NewResolver([]string{"8.8.8.8:53"})

	// 手动填充缓存
	r.mu.Lock()
	r.cache["test.example.com"] = &cacheItem{ips: nil}
	r.cache["test2.example.com"] = &cacheItem{ips: nil}
	r.mu.Unlock()

	r.mu.RLock()
	before := len(r.cache)
	r.mu.RUnlock()
	if before != 2 {
		t.Errorf("expected 2 cache entries before clear, got %d", before)
	}

	r.ClearCache()

	r.mu.RLock()
	count := len(r.cache)
	r.mu.RUnlock()

	if count != 0 {
		t.Errorf("expected empty cache after ClearCache, got %d entries", count)
	}
}

func TestResolveIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	r := NewResolver([]string{"8.8.8.8:53", "1.1.1.1:53"})
	ips, err := r.Resolve("example.com")
	if err != nil {
		t.Skipf("DNS resolve failed (network issue?): %v", err)
	}
	if len(ips) == 0 {
		t.Error("Resolve returned empty IP list")
	}

	// 测试缓存命中
	ips2, err := r.Resolve("example.com")
	if err != nil {
		t.Errorf("second resolve error: %v", err)
	}
	if len(ips2) == 0 {
		t.Error("cached resolve returned empty IP list")
	}
}
