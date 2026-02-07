package mcp

import (
	"sync"
	"testing"
)

func TestClientBase_Tools_Empty(t *testing.T) {
	b := &ClientBase{}
	if got := b.Tools(); got != nil {
		t.Errorf("expected nil tools, got %v", got)
	}
}

func TestClientBase_SetTools_NoWhitelist(t *testing.T) {
	b := &ClientBase{}
	tools := []Tool{
		{Name: "tool-a"},
		{Name: "tool-b"},
	}
	b.SetTools(tools)

	got := b.Tools()
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(got))
	}
	if got[0].Name != "tool-a" || got[1].Name != "tool-b" {
		t.Errorf("unexpected tools: %v", got)
	}
}

func TestClientBase_SetTools_WithWhitelist(t *testing.T) {
	b := &ClientBase{}
	b.SetToolWhitelist([]string{"allowed"})
	b.SetTools([]Tool{
		{Name: "allowed"},
		{Name: "blocked"},
	})

	got := b.Tools()
	if len(got) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(got))
	}
	if got[0].Name != "allowed" {
		t.Errorf("expected 'allowed' tool, got %q", got[0].Name)
	}
}

func TestClientBase_SetTools_WhitelistFiltersAll(t *testing.T) {
	b := &ClientBase{}
	b.SetToolWhitelist([]string{"nonexistent"})
	b.SetTools([]Tool{
		{Name: "tool-a"},
		{Name: "tool-b"},
	})

	got := b.Tools()
	if len(got) != 0 {
		t.Errorf("expected 0 tools, got %d", len(got))
	}
}

func TestClientBase_IsInitialized_Default(t *testing.T) {
	b := &ClientBase{}
	if b.IsInitialized() {
		t.Error("expected not initialized by default")
	}
}

func TestClientBase_SetInitialized(t *testing.T) {
	b := &ClientBase{}
	info := ServerInfo{Name: "test-server", Version: "1.0.0"}
	b.SetInitialized(info)

	if !b.IsInitialized() {
		t.Error("expected initialized after SetInitialized")
	}
	if got := b.ServerInfo(); got != info {
		t.Errorf("expected server info %v, got %v", info, got)
	}
}

func TestClientBase_ServerInfo_Default(t *testing.T) {
	b := &ClientBase{}
	if got := b.ServerInfo(); got != (ServerInfo{}) {
		t.Errorf("expected zero ServerInfo, got %v", got)
	}
}

func TestClientBase_SetToolWhitelist(t *testing.T) {
	b := &ClientBase{}

	// Set whitelist then add tools
	b.SetToolWhitelist([]string{"x", "y"})
	b.SetTools([]Tool{
		{Name: "x"},
		{Name: "y"},
		{Name: "z"},
	})

	got := b.Tools()
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(got))
	}

	// Clear whitelist and reset tools
	b.SetToolWhitelist(nil)
	b.SetTools([]Tool{
		{Name: "x"},
		{Name: "y"},
		{Name: "z"},
	})

	got = b.Tools()
	if len(got) != 3 {
		t.Fatalf("expected 3 tools after clearing whitelist, got %d", len(got))
	}
}

func TestFilterTools(t *testing.T) {
	tests := []struct {
		name      string
		tools     []Tool
		whitelist []string
		want      int
	}{
		{
			name:      "all match",
			tools:     []Tool{{Name: "a"}, {Name: "b"}},
			whitelist: []string{"a", "b"},
			want:      2,
		},
		{
			name:      "partial match",
			tools:     []Tool{{Name: "a"}, {Name: "b"}, {Name: "c"}},
			whitelist: []string{"a", "c"},
			want:      2,
		},
		{
			name:      "no match",
			tools:     []Tool{{Name: "a"}, {Name: "b"}},
			whitelist: []string{"x"},
			want:      0,
		},
		{
			name:      "empty tools",
			tools:     nil,
			whitelist: []string{"a"},
			want:      0,
		},
		{
			name:      "nil whitelist matches nothing",
			tools:     []Tool{{Name: "a"}},
			whitelist: nil,
			want:      0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterTools(tc.tools, tc.whitelist)
			if len(got) != tc.want {
				t.Errorf("filterTools() returned %d tools, want %d", len(got), tc.want)
			}
		})
	}
}

func TestClientBase_ConcurrentAccess(t *testing.T) {
	b := &ClientBase{}
	var wg sync.WaitGroup

	// Concurrent writers and readers
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			b.SetTools([]Tool{{Name: "tool"}})
		}()
		go func() {
			defer wg.Done()
			b.SetInitialized(ServerInfo{Name: "server", Version: "1.0"})
		}()
		go func() {
			defer wg.Done()
			_ = b.Tools()
			_ = b.IsInitialized()
			_ = b.ServerInfo()
		}()
	}

	wg.Wait()
}
