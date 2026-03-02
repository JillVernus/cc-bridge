package config

import "testing"

func findCompositeMappingByPattern(mappings []CompositeMapping, pattern string) *CompositeMapping {
	for i := range mappings {
		if mappings[i].Pattern == pattern {
			return &mappings[i]
		}
	}
	return nil
}

func TestNormalizeCompositeMappingTargets_LegacyFailoverChainDefaultsToMessagesPool(t *testing.T) {
	mapping := CompositeMapping{
		Pattern:         "haiku",
		TargetChannelID: "msg-primary",
		FailoverChain:   []string{"msg-failover-1", "msg-failover-2"},
	}

	normalizeCompositeMappingTargets(&mapping)

	if mapping.TargetPool != CompositeTargetPoolMessages {
		t.Fatalf("expected targetPool=%q, got %q", CompositeTargetPoolMessages, mapping.TargetPool)
	}
	if len(mapping.FailoverTargets) != 2 {
		t.Fatalf("expected 2 failoverTargets, got %d", len(mapping.FailoverTargets))
	}
	for i, target := range mapping.FailoverTargets {
		if target.Pool != CompositeTargetPoolMessages {
			t.Fatalf("expected failoverTargets[%d].pool=%q, got %q", i, CompositeTargetPoolMessages, target.Pool)
		}
		if target.ChannelID == "" {
			t.Fatalf("expected failoverTargets[%d].channelId to be non-empty", i)
		}
	}
	if len(mapping.FailoverChain) != 2 || mapping.FailoverChain[0] != "msg-failover-1" || mapping.FailoverChain[1] != "msg-failover-2" {
		t.Fatalf("expected legacy failoverChain to be preserved as channel IDs, got %#v", mapping.FailoverChain)
	}
}

func TestGetCompositeFailoverTargetRefs_PrefersFailoverTargetsOverLegacyChain(t *testing.T) {
	mapping := CompositeMapping{
		Pattern:         "haiku",
		TargetPool:      CompositeTargetPoolResponses,
		TargetChannelID: "resp-primary",
		FailoverTargets: []CompositeTargetRef{
			{Pool: CompositeTargetPoolMessages, ChannelID: "msg-canonical"},
		},
		FailoverChain: []string{"resp-legacy-should-be-ignored"},
	}

	targets := getCompositeFailoverTargetRefs(&mapping)
	if len(targets) != 1 {
		t.Fatalf("expected 1 canonical failover target, got %d", len(targets))
	}
	if targets[0].Pool != CompositeTargetPoolMessages {
		t.Fatalf("expected canonical failover pool=%q, got %q", CompositeTargetPoolMessages, targets[0].Pool)
	}
	if targets[0].ChannelID != "msg-canonical" {
		t.Fatalf("expected canonical failover channelId=msg-canonical, got %q", targets[0].ChannelID)
	}
}

func TestResolveCompositeMappingWithPools_ResolvesAcrossPools(t *testing.T) {
	messages := []UpstreamConfig{
		{ID: "msg-primary", Name: "Messages Primary"},
		{ID: "msg-fallback", Name: "Messages Fallback"},
	}
	responses := []UpstreamConfig{
		{ID: "resp-primary", Name: "Responses Primary"},
	}

	composite := &UpstreamConfig{
		ID:          "cmp-1",
		ServiceType: "composite",
		CompositeMappings: []CompositeMapping{
			{
				Pattern:         "haiku",
				TargetPool:      CompositeTargetPoolResponses,
				TargetChannelID: "resp-primary",
				FailoverChain:   []string{"msg-fallback"},
			},
			{
				Pattern:         "sonnet",
				TargetChannelID: "msg-primary", // legacy: missing targetPool should default to messages
				FailoverChain:   []string{"msg-fallback"},
			},
			{
				Pattern:         "opus",
				TargetChannelID: "msg-primary",
				FailoverChain:   []string{"msg-fallback"},
			},
		},
	}

	haikuResolved, found := ResolveCompositeMappingWithPools(composite, "haiku", messages, responses)
	if !found {
		t.Fatalf("expected haiku mapping to resolve")
	}
	if haikuResolved.TargetPool != CompositeTargetPoolResponses {
		t.Fatalf("expected haiku targetPool=%q, got %q", CompositeTargetPoolResponses, haikuResolved.TargetPool)
	}
	if haikuResolved.TargetChannelID != "resp-primary" {
		t.Fatalf("expected haiku targetChannelId=resp-primary, got %q", haikuResolved.TargetChannelID)
	}
	if haikuResolved.TargetIndex != 0 {
		t.Fatalf("expected haiku targetIndex=0 in responses pool, got %d", haikuResolved.TargetIndex)
	}

	sonnetResolved, found := ResolveCompositeMappingWithPools(composite, "sonnet", messages, responses)
	if !found {
		t.Fatalf("expected sonnet mapping to resolve")
	}
	if sonnetResolved.TargetPool != CompositeTargetPoolMessages {
		t.Fatalf("expected sonnet targetPool=%q, got %q", CompositeTargetPoolMessages, sonnetResolved.TargetPool)
	}
	if sonnetResolved.TargetChannelID != "msg-primary" {
		t.Fatalf("expected sonnet targetChannelId=msg-primary, got %q", sonnetResolved.TargetChannelID)
	}
	if sonnetResolved.TargetIndex != 0 {
		t.Fatalf("expected sonnet targetIndex=0 in messages pool, got %d", sonnetResolved.TargetIndex)
	}
}

func TestResolveCompositeMappingWithPools_LegacyPrimaryTargetChannelIndexFallback(t *testing.T) {
	messages := []UpstreamConfig{
		{ID: "msg-0", Name: "Messages 0"},
		{ID: "msg-1", Name: "Messages 1"},
	}

	composite := &UpstreamConfig{
		ID:          "cmp-legacy-primary",
		ServiceType: "composite",
		CompositeMappings: []CompositeMapping{
			{
				Pattern:       "haiku",
				TargetChannel: 1, // legacy index-only primary mapping
			},
		},
	}

	resolved, found := ResolveCompositeMappingWithPools(composite, "haiku", messages, nil)
	if !found {
		t.Fatalf("expected legacy targetChannel mapping to resolve")
	}
	if resolved.TargetPool != CompositeTargetPoolMessages {
		t.Fatalf("expected legacy mapping targetPool=%q, got %q", CompositeTargetPoolMessages, resolved.TargetPool)
	}
	if resolved.TargetIndex != 1 {
		t.Fatalf("expected legacy mapping targetIndex=1, got %d", resolved.TargetIndex)
	}
	if resolved.TargetChannelID != "msg-1" {
		t.Fatalf("expected legacy mapping targetChannelId=msg-1, got %q", resolved.TargetChannelID)
	}
}

func TestUpdateCompositeMappingsOnDelete_RemovesMessagesPoolTargets(t *testing.T) {
	cm := &ConfigManager{
		config: Config{
			Upstream: []UpstreamConfig{
				{
					ID:          "cmp-main",
					ServiceType: "composite",
					CompositeMappings: []CompositeMapping{
						{
							Pattern:         "haiku",
							TargetChannelID: "msg-delete",
							FailoverTargets: []CompositeTargetRef{
								{Pool: CompositeTargetPoolMessages, ChannelID: "msg-keep"},
							},
						},
						{
							Pattern:         "sonnet",
							TargetChannelID: "msg-keep",
							FailoverTargets: []CompositeTargetRef{
								{Pool: CompositeTargetPoolMessages, ChannelID: "msg-delete"},
							},
						},
						{
							Pattern:         "opus",
							TargetChannelID: "msg-opus",
							FailoverTargets: []CompositeTargetRef{
								{Pool: CompositeTargetPoolMessages, ChannelID: "msg-keep"},
							},
						},
					},
				},
			},
		},
	}

	cm.updateCompositeMappingsOnDelete("msg-delete", 1, CompositeTargetPoolMessages)

	mappings := cm.config.Upstream[0].CompositeMappings
	if len(mappings) != 2 {
		t.Fatalf("expected 2 mappings after removing messages primary target, got %d", len(mappings))
	}
	if findCompositeMappingByPattern(mappings, "haiku") != nil {
		t.Fatalf("expected haiku mapping to be removed because its primary target was deleted")
	}

	sonnet := findCompositeMappingByPattern(mappings, "sonnet")
	if sonnet == nil {
		t.Fatalf("expected sonnet mapping to remain")
	}
	for _, target := range sonnet.FailoverTargets {
		if target.ChannelID == "msg-delete" && NormalizeCompositeTargetPool(target.Pool) == CompositeTargetPoolMessages {
			t.Fatalf("expected deleted messages target to be removed from sonnet failoverTargets")
		}
	}
	for _, id := range sonnet.FailoverChain {
		if id == "msg-delete" {
			t.Fatalf("expected deleted messages target to be removed from sonnet failoverChain")
		}
	}
}

func TestUpdateCompositeMappingsOnDelete_RemovesResponsesPoolTargets(t *testing.T) {
	cm := &ConfigManager{
		config: Config{
			Upstream: []UpstreamConfig{
				{
					ID:          "cmp-main",
					ServiceType: "composite",
					CompositeMappings: []CompositeMapping{
						{
							Pattern:         "haiku",
							TargetPool:      CompositeTargetPoolResponses,
							TargetChannelID: "resp-delete",
							FailoverTargets: []CompositeTargetRef{
								{Pool: CompositeTargetPoolResponses, ChannelID: "resp-keep"},
							},
						},
						{
							Pattern:         "sonnet",
							TargetPool:      CompositeTargetPoolMessages,
							TargetChannelID: "msg-keep",
							FailoverTargets: []CompositeTargetRef{
								{Pool: CompositeTargetPoolResponses, ChannelID: "resp-delete"},
								{Pool: CompositeTargetPoolMessages, ChannelID: "msg-opus"},
							},
						},
						{
							Pattern:         "opus",
							TargetPool:      CompositeTargetPoolMessages,
							TargetChannelID: "msg-opus",
							FailoverTargets: []CompositeTargetRef{
								{Pool: CompositeTargetPoolMessages, ChannelID: "msg-keep"},
							},
						},
					},
				},
			},
		},
	}

	cm.updateCompositeMappingsOnDelete("resp-delete", 0, CompositeTargetPoolResponses)

	mappings := cm.config.Upstream[0].CompositeMappings
	if len(mappings) != 2 {
		t.Fatalf("expected 2 mappings after removing responses primary target, got %d", len(mappings))
	}
	if findCompositeMappingByPattern(mappings, "haiku") != nil {
		t.Fatalf("expected haiku mapping to be removed because its responses primary target was deleted")
	}

	sonnet := findCompositeMappingByPattern(mappings, "sonnet")
	if sonnet == nil {
		t.Fatalf("expected sonnet mapping to remain")
	}
	if len(sonnet.FailoverTargets) != 1 {
		t.Fatalf("expected sonnet to keep 1 failover target after responses deletion, got %d", len(sonnet.FailoverTargets))
	}
	if sonnet.FailoverTargets[0].ChannelID != "msg-opus" || NormalizeCompositeTargetPool(sonnet.FailoverTargets[0].Pool) != CompositeTargetPoolMessages {
		t.Fatalf("expected remaining sonnet failover target to be messages/msg-opus, got %+v", sonnet.FailoverTargets[0])
	}
	if len(sonnet.FailoverChain) != 1 || sonnet.FailoverChain[0] != "msg-opus" {
		t.Fatalf("expected legacy failoverChain to be updated to [msg-opus], got %#v", sonnet.FailoverChain)
	}
}
