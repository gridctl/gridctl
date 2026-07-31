package mcp

import "testing"

func TestSupportedProtocolVersions_NewestFirst(t *testing.T) {
	if len(SupportedProtocolVersions) == 0 {
		t.Fatal("SupportedProtocolVersions must not be empty")
	}
	if SupportedProtocolVersions[0] != StatelessProtocolVersion {
		t.Errorf("expected first entry %q to be the newest revision %q",
			SupportedProtocolVersions[0], StatelessProtocolVersion)
	}
	// MCPProtocolVersion is the handshake-era default and must stay a
	// supported handshake-era member, never a stateless revision.
	if !IsSupportedProtocolVersion(MCPProtocolVersion) {
		t.Errorf("MCPProtocolVersion %q must be supported", MCPProtocolVersion)
	}
	if EraOfVersion(MCPProtocolVersion) != EraHandshake {
		t.Errorf("MCPProtocolVersion %q must be handshake-era", MCPProtocolVersion)
	}
}

func TestIsSupportedProtocolVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"2025-11-25", true},
		{"2025-06-18", true},
		{"2025-03-26", true},
		{"2024-11-05", true},
		{"2026-07-28", true},
		{"1999-01-01", false},
		{"", false},
		{"not-a-version", false},
	}
	for _, tt := range tests {
		if got := IsSupportedProtocolVersion(tt.version); got != tt.want {
			t.Errorf("IsSupportedProtocolVersion(%q) = %v, want %v", tt.version, got, tt.want)
		}
	}
}

func TestNegotiateProtocolVersion(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		want      string
	}{
		{"echo latest", "2025-11-25", "2025-11-25"},
		{"echo older supported", "2025-06-18", "2025-06-18"},
		{"echo oldest supported", "2024-11-05", "2024-11-05"},
		{"counter-offer on unknown", "1999-01-01", MCPProtocolVersion},
		// A stateless revision has no initialize; a handshake client
		// requesting it gets the latest handshake-era version back.
		{"counter-offer on stateless revision", "2026-07-28", MCPProtocolVersion},
		{"counter-offer on empty", "", MCPProtocolVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NegotiateProtocolVersion(tt.requested); got != tt.want {
				t.Errorf("NegotiateProtocolVersion(%q) = %q, want %q", tt.requested, got, tt.want)
			}
		})
	}
}
