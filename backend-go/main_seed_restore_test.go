package main

import (
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
)

func TestBuildChannelIDIndex(t *testing.T) {
	upstreams := []config.UpstreamConfig{
		{ID: "a", Name: "Alpha"},
		{ID: "", Name: "NoID"},
		{ID: "b", Name: "Beta"},
		{ID: " a ", Name: "DupAlpha"},
	}

	m := buildChannelIDIndex(upstreams)
	if got := m["a"]; got != 3 {
		t.Fatalf("expected last write wins for 'a' (idx=3), got %d", got)
	}
	if got := m["b"]; got != 2 {
		t.Fatalf("expected b idx=2, got %d", got)
	}
	if _, ok := m[""]; ok {
		t.Fatalf("expected empty id excluded")
	}
}

func TestBuildUniqueChannelNameIndex(t *testing.T) {
	upstreams := []config.UpstreamConfig{
		{Name: "Alpha"},
		{Name: "  beta  "},
		{Name: "ALPHA"},
		{Name: ""},
		{Name: "Gamma"},
	}

	m := buildUniqueChannelNameIndex(upstreams)

	if _, exists := m["alpha"]; exists {
		t.Fatalf("expected duplicate name 'alpha' to be excluded")
	}
	if got := m["beta"]; got != 1 {
		t.Fatalf("expected beta index=1, got %d", got)
	}
	if got := m["gamma"]; got != 4 {
		t.Fatalf("expected gamma index=4, got %d", got)
	}
}

func TestResolveSeedTargetIndex(t *testing.T) {
	t.Run("prefer uid", func(t *testing.T) {
		idx, by := resolveSeedTargetIndex("uid-1", 0, "beta", map[string]int{"uid-1": 4}, map[string]int{"beta": 3}, 5)
		if idx != 4 || by != "uid" {
			t.Fatalf("expected uid index=4,uid, got %d,%q", idx, by)
		}
	})

	t.Run("prefer mapped name", func(t *testing.T) {
		idx, by := resolveSeedTargetIndex("", 0, "beta", nil, map[string]int{"beta": 3}, 5)
		if idx != 3 || by != "name" {
			t.Fatalf("expected name index=3,name, got %d,%q", idx, by)
		}
	})

	t.Run("fallback to bounded channel id", func(t *testing.T) {
		idx, by := resolveSeedTargetIndex("", 2, "unknown", nil, map[string]int{"beta": 3}, 5)
		if idx != 2 || by != "index" {
			t.Fatalf("expected fallback index=2,index, got %d,%q", idx, by)
		}
	})

	t.Run("reject out of bounds when count known", func(t *testing.T) {
		idx, by := resolveSeedTargetIndex("", 9, "unknown", nil, nil, 5)
		if idx != -1 || by != "" {
			t.Fatalf("expected out-of-bounds to be rejected, got %d,%q", idx, by)
		}
	})

	t.Run("accept non-negative when count unknown", func(t *testing.T) {
		idx, by := resolveSeedTargetIndex("", 9, "unknown", nil, nil, 0)
		if idx != 9 || by != "index" {
			t.Fatalf("expected unknown-count fallback index=9,index, got %d,%q", idx, by)
		}
	})
}
