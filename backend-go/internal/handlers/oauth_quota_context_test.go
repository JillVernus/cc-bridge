package handlers

import (
	"testing"

	"github.com/JillVernus/cc-bridge/internal/config"
)

func TestResolveOAuthQuotaUpdateContext_PrefersSelectedChannelContext(t *testing.T) {
	upstream := &config.UpstreamConfig{
		Index: 0,
		ID:    "4a8081d3",
		Name:  "Plus:jj-vpn-2023",
	}

	channelIndex, stableID, channelName := resolveOAuthQuotaUpdateContext(upstream, 9, "Plus:jj-vpn-2023")

	if channelIndex != 9 {
		t.Fatalf("channelIndex = %d, want 9", channelIndex)
	}
	if stableID != "4a8081d3" {
		t.Fatalf("stableID = %q, want %q", stableID, "4a8081d3")
	}
	if channelName != "Plus:jj-vpn-2023" {
		t.Fatalf("channelName = %q, want %q", channelName, "Plus:jj-vpn-2023")
	}
}

func TestResolveOAuthQuotaUpdateContext_FallsBackToUpstreamContext(t *testing.T) {
	upstream := &config.UpstreamConfig{
		Index: 3,
		ID:    "24370f65",
		Name:  "Plus:disney_plus_j",
	}

	channelIndex, stableID, channelName := resolveOAuthQuotaUpdateContext(upstream, -1, "")

	if channelIndex != 3 {
		t.Fatalf("channelIndex = %d, want 3", channelIndex)
	}
	if stableID != "24370f65" {
		t.Fatalf("stableID = %q, want %q", stableID, "24370f65")
	}
	if channelName != "Plus:disney_plus_j" {
		t.Fatalf("channelName = %q, want %q", channelName, "Plus:disney_plus_j")
	}
}
