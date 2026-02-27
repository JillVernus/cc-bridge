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

func TestEnsureUniqueChannelName_AllowsSameNameAcrossTypes(t *testing.T) {
	cm := &ConfigManager{}
	cm.config.Upstream = []UpstreamConfig{{Name: "Shared", ID: "id-a"}}

	// Same name in a different type should be allowed
	if err := cm.ensureUniqueChannelNameLocked("Shared", "gemini", -1); err != nil {
		t.Fatalf("expected same name across types to be allowed, got: %v", err)
	}
	if err := cm.ensureUniqueChannelNameLocked("Shared", "responses", -1); err != nil {
		t.Fatalf("expected same name across types to be allowed, got: %v", err)
	}
}

func TestEnsureUniqueChannelName_AllowsSameNameAcrossAllTypes(t *testing.T) {
	cm := &ConfigManager{}
	cm.config.Upstream = []UpstreamConfig{{Name: "Shared", ID: "id-a"}}
	cm.config.ResponsesUpstream = []UpstreamConfig{{Name: "Other", ID: "id-b"}}
	cm.config.GeminiUpstream = []UpstreamConfig{{Name: "Third", ID: "id-c"}}

	// "Shared" exists in messages — should be allowed in responses and gemini
	if err := cm.ensureUniqueChannelNameLocked("shared", "responses", -1); err != nil {
		t.Fatalf("expected same name from messages to be allowed in responses, got: %v", err)
	}
	if err := cm.ensureUniqueChannelNameLocked("shared", "gemini", -1); err != nil {
		t.Fatalf("expected same name from messages to be allowed in gemini, got: %v", err)
	}

	// "Other" exists in responses — should be allowed in messages and gemini
	if err := cm.ensureUniqueChannelNameLocked("other", "messages", -1); err != nil {
		t.Fatalf("expected same name from responses to be allowed in messages, got: %v", err)
	}
	if err := cm.ensureUniqueChannelNameLocked("other", "gemini", -1); err != nil {
		t.Fatalf("expected same name from responses to be allowed in gemini, got: %v", err)
	}
}

func TestEnsureUniqueChannelName_RejectsSameNameWithinType(t *testing.T) {
	cm := &ConfigManager{}
	cm.config.Upstream = []UpstreamConfig{{Name: "Alpha", ID: "id-a"}}

	if err := cm.ensureUniqueChannelNameLocked("alpha", "messages", -1); err == nil {
		t.Fatalf("expected duplicate name within same type to be rejected")
	}
}
