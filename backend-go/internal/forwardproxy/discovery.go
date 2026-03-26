package forwardproxy

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	DiscoveryTransportConnect     = "connect"
	DiscoveryTransportHTTPForward = "http_forward"
	DiscoveryTransportMITM        = "mitm"
)

type DiscoveryEvent struct {
	Host           string
	Port           string
	Transport      string
	Intercepted    bool
	Method         string
	Path           string
	RequestHeaders http.Header
	SeenAt         time.Time
}

type DiscoveryEntry struct {
	Host               string            `json:"host"`
	Port               string            `json:"port"`
	Transport          string            `json:"transport"`
	Intercepted        bool              `json:"intercepted"`
	SeenCount          int               `json:"seenCount"`
	FirstSeenAt        time.Time         `json:"firstSeenAt"`
	LastSeenAt         time.Time         `json:"lastSeenAt"`
	LastMethod         string            `json:"lastMethod,omitempty"`
	LastPath           string            `json:"lastPath,omitempty"`
	LastRequestHeaders map[string]string `json:"lastRequestHeaders,omitempty"`
}

type DiscoveryStore struct {
	mu      sync.RWMutex
	path    string
	entries map[string]DiscoveryEntry
}

func NewDiscoveryStore(path string) (*DiscoveryStore, error) {
	store := &DiscoveryStore{
		path:    path,
		entries: make(map[string]DiscoveryEntry),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *DiscoveryStore) Record(event DiscoveryEvent) {
	if s == nil {
		return
	}

	host := strings.ToLower(strings.TrimSpace(event.Host))
	port := strings.TrimSpace(event.Port)
	if host == "" {
		return
	}
	if port == "" {
		port = "443"
	}
	seenAt := event.SeenAt.UTC()
	if seenAt.IsZero() {
		seenAt = time.Now().UTC()
	}

	key := host + ":" + port

	s.mu.Lock()
	entry, exists := s.entries[key]
	if !exists {
		entry = DiscoveryEntry{
			Host:        host,
			Port:        port,
			FirstSeenAt: seenAt,
		}
	}
	entry.Transport = strings.TrimSpace(event.Transport)
	entry.Intercepted = entry.Intercepted || event.Intercepted
	entry.SeenCount++
	if entry.FirstSeenAt.IsZero() || seenAt.Before(entry.FirstSeenAt) {
		entry.FirstSeenAt = seenAt
	}
	if entry.LastSeenAt.IsZero() || seenAt.After(entry.LastSeenAt) {
		entry.LastSeenAt = seenAt
	}
	if method := strings.TrimSpace(event.Method); method != "" {
		entry.LastMethod = method
	}
	if path := strings.TrimSpace(event.Path); path != "" {
		entry.LastPath = path
	}
	if len(event.RequestHeaders) > 0 {
		entry.LastRequestHeaders = sanitizeDiscoveryHeaders(event.RequestHeaders)
	}
	s.entries[key] = entry
	err := s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		// Best effort only; do not break proxy traffic on discovery persistence failure.
		return
	}
}

func sanitizeDiscoveryHeaders(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	result := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) == 0 {
			continue
		}
		joined := strings.Join(values, ", ")
		lowerKey := strings.ToLower(strings.TrimSpace(key))
		switch lowerKey {
		case "authorization", "x-api-key", "cookie", "set-cookie", "proxy-authorization":
			if len(joined) > 12 {
				result[key] = joined[:8] + "..." + joined[len(joined)-4:]
			} else {
				result[key] = "***"
			}
		default:
			result[key] = joined
		}
	}
	return result
}

func (s *DiscoveryStore) List() []DiscoveryEntry {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := make([]DiscoveryEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].LastSeenAt.Equal(entries[j].LastSeenAt) {
			if entries[i].SeenCount == entries[j].SeenCount {
				return entries[i].Host < entries[j].Host
			}
			return entries[i].SeenCount > entries[j].SeenCount
		}
		return entries[i].LastSeenAt.After(entries[j].LastSeenAt)
	})
	return entries
}

func (s *DiscoveryStore) Clear() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.entries = make(map[string]DiscoveryEntry)
	err := s.saveLocked()
	s.mu.Unlock()
	return err
}

func (s *DiscoveryStore) load() error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var entries []DiscoveryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Host == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(entry.Host)) + ":" + strings.TrimSpace(entry.Port)
		s.entries[key] = entry
	}
	return nil
}

func (s *DiscoveryStore) saveLocked() error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return nil
	}
	entries := make([]DiscoveryEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Host == entries[j].Host {
			return entries[i].Port < entries[j].Port
		}
		return entries[i].Host < entries[j].Host
	})
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}
