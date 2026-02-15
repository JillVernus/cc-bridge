package config

import "testing"

func TestAddUpstream_RejectsDuplicateNameCaseInsensitive(t *testing.T) {
	cm := &ConfigManager{}
	cm.config.Upstream = []UpstreamConfig{{Name: "Alpha", ID: "id-a"}}

	if err := cm.AddUpstream(UpstreamConfig{Name: " alpha ", BaseURL: "x", ServiceType: "openai"}); err == nil {
		t.Fatalf("expected duplicate name error")
	}
}

func TestUpdateUpstream_RejectsDuplicateNameCaseInsensitive(t *testing.T) {
	cm := &ConfigManager{}
	cm.config.Upstream = []UpstreamConfig{{Name: "Alpha", ID: "id-a"}, {Name: "Beta", ID: "id-b"}}

	name := "ALPHA"
	_, err := cm.UpdateUpstream(1, UpstreamUpdate{Name: &name})
	if err == nil {
		t.Fatalf("expected duplicate name error")
	}
}

func TestAddResponsesUpstream_RejectsEmptyName(t *testing.T) {
	cm := &ConfigManager{}
	if err := cm.AddResponsesUpstream(UpstreamConfig{Name: "  ", BaseURL: "x", ServiceType: "openai"}); err == nil {
		t.Fatalf("expected empty name to be rejected")
	}
}

func TestEnsureUniqueChannelName_RejectsSameNameAcrossTypes(t *testing.T) {
	cm := &ConfigManager{}
	cm.config.Upstream = []UpstreamConfig{{Name: "Shared", ID: "id-a"}}
	if err := cm.ensureUniqueChannelNameLocked("Shared", "gemini", -1); err == nil {
		t.Fatalf("expected duplicate name across types to be rejected")
	}
}

func TestAddResponsesUpstream_RejectsDuplicateFromMessages(t *testing.T) {
	cm := &ConfigManager{}
	cm.config.Upstream = []UpstreamConfig{{Name: "Shared", ID: "id-a"}}
	if err := cm.AddResponsesUpstream(UpstreamConfig{Name: "shared", BaseURL: "x", ServiceType: "openai"}); err == nil {
		t.Fatalf("expected duplicate name across channel types to be rejected")
	}
}
