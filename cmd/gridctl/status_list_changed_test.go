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

func TestMCPServerAPI_SubscriptionStream(t *testing.T) {
	var server mcpServerAPI
	if err := json.Unmarshal([]byte(`{"name":"echo","subscriptionStream":"open"}`), &server); err != nil {
		t.Fatal(err)
	}
	if server.SubscriptionStream != "open" {
		t.Fatalf("subscriptionStream = %q", server.SubscriptionStream)
	}
	var omitted mcpServerAPI
	if err := json.Unmarshal([]byte(`{"name":"legacy"}`), &omitted); err != nil {
		t.Fatal(err)
	}
	if omitted.SubscriptionStream != "" {
		t.Fatalf("omitted subscriptionStream = %q", omitted.SubscriptionStream)
	}
}
