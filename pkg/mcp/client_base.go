package mcp

import (
	"sync"
)

// ClientBase provides shared state and accessor methods for all AgentClient implementations.
// Embed this struct to get Tools(), IsInitialized(), ServerInfo(), and SetToolWhitelist().
type ClientBase struct {
	mu            sync.RWMutex
	initialized   bool
	tools         []Tool
	serverInfo    ServerInfo
	toolWhitelist []string
}

// Tools returns the cached tool list.
func (b *ClientBase) Tools() []Tool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.tools
}

// IsInitialized returns whether the client has been initialized.
func (b *ClientBase) IsInitialized() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.initialized
}

// ServerInfo returns the server information.
func (b *ClientBase) ServerInfo() ServerInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.serverInfo
}

// SetToolWhitelist sets the list of allowed tool names.
// Only tools in this list will be returned by Tools() and RefreshTools().
// An empty or nil list means all tools are allowed.
func (b *ClientBase) SetToolWhitelist(tools []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.toolWhitelist = tools
}

// SetTools updates the cached tools, applying the whitelist filter.
func (b *ClientBase) SetTools(tools []Tool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.toolWhitelist) > 0 {
		b.tools = filterTools(tools, b.toolWhitelist)
	} else {
		b.tools = tools
	}
}

// SetInitialized marks the client as initialized with the given server info.
func (b *ClientBase) SetInitialized(info ServerInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.initialized = true
	b.serverInfo = info
}

// filterTools returns only tools whose names are in the whitelist.
func filterTools(tools []Tool, whitelist []string) []Tool {
	allowed := make(map[string]bool, len(whitelist))
	for _, name := range whitelist {
		allowed[name] = true
	}
	var filtered []Tool
	for _, tool := range tools {
		if allowed[tool.Name] {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}
