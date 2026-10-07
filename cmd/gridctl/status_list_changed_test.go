package main

import (
	"encoding/json"
	"testing"
)

func TestMCPCapabilityAPI_ToolsListChanged(t *testing.T) {
	var caps mcpCapabilityAPI
	if err := json.Unmarshal([]byte(`{"prompts":false,"resources":false,"resourcesSubscribe":false,"resourcesListChanged":false,"toolsListChanged":true}`), &caps); err != nil {
		t.Fatal(err)
	}
	if !caps.ToolsListChanged {
		t.Fatal("toolsListChanged was not decoded")
	}
}
