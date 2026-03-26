package forwardproxy

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoveryStore_RecordAndPersist(t *testing.T) {
	store, err := NewDiscoveryStore(filepath.Join(t.TempDir(), "forward-proxy-discovery.json"))
	if err != nil {
		t.Fatalf("NewDiscoveryStore failed: %v", err)
	}

	firstSeen := time.Date(2026, 3, 26, 8, 0, 0, 0, time.UTC)
	secondSeen := firstSeen.Add(2 * time.Minute)

	store.Record(DiscoveryEvent{
		Host:        "api.openai.com",
		Port:        "443",
		Transport:   DiscoveryTransportConnect,
		Intercepted: false,
		SeenAt:      firstSeen,
	})
	store.Record(DiscoveryEvent{
		Host:        "api.openai.com",
		Port:        "443",
		Transport:   DiscoveryTransportHTTPForward,
		Intercepted: true,
		Method:      "POST",
		Path:        "/v1/responses",
		SeenAt:      secondSeen,
	})

	entries := store.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 discovery entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Host != "api.openai.com" {
		t.Fatalf("expected host api.openai.com, got %q", entry.Host)
	}
	if entry.Port != "443" {
		t.Fatalf("expected port 443, got %q", entry.Port)
	}
	if entry.SeenCount != 2 {
		t.Fatalf("expected seenCount 2, got %d", entry.SeenCount)
	}
	if !entry.Intercepted {
		t.Fatalf("expected intercepted=true after intercepted sighting")
	}
	if entry.LastMethod != "POST" {
		t.Fatalf("expected lastMethod POST, got %q", entry.LastMethod)
	}
	if entry.LastPath != "/v1/responses" {
		t.Fatalf("expected lastPath /v1/responses, got %q", entry.LastPath)
	}
	if !entry.FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("expected firstSeenAt %v, got %v", firstSeen, entry.FirstSeenAt)
	}
	if !entry.LastSeenAt.Equal(secondSeen) {
		t.Fatalf("expected lastSeenAt %v, got %v", secondSeen, entry.LastSeenAt)
	}

	reloaded, err := NewDiscoveryStore(store.path)
	if err != nil {
		t.Fatalf("reloading discovery store failed: %v", err)
	}
	reloadedEntries := reloaded.List()
	if len(reloadedEntries) != 1 || reloadedEntries[0].SeenCount != 2 {
		t.Fatalf("expected persisted entry with seenCount 2, got %#v", reloadedEntries)
	}
}

func TestDiscoveryStore_Clear(t *testing.T) {
	store, err := NewDiscoveryStore(filepath.Join(t.TempDir(), "forward-proxy-discovery.json"))
	if err != nil {
		t.Fatalf("NewDiscoveryStore failed: %v", err)
	}

	store.Record(DiscoveryEvent{
		Host:        "api.anthropic.com",
		Port:        "443",
		Transport:   DiscoveryTransportConnect,
		Intercepted: false,
		SeenAt:      time.Now().UTC(),
	})
	if len(store.List()) != 1 {
		t.Fatalf("expected entry before clear")
	}

	if err := store.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}
	if got := len(store.List()); got != 0 {
		t.Fatalf("expected 0 entries after clear, got %d", got)
	}
}
