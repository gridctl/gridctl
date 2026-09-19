package mcp

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/a2aclient"
)

type a2aResultMessage struct {
	Role  string           `json:"role"`
	Parts []a2aclient.Part `json:"parts"`
}

type a2aResultArtifact struct {
	Name        string           `json:"name,omitempty"`
	Description string           `json:"description,omitempty"`
	Parts       []a2aclient.Part `json:"parts"`
}

type a2aEnvelope struct {
	Kind             string              `json:"kind"`
	TaskHandle       string              `json:"task_handle,omitempty"`
	ContextHandle    string              `json:"context_handle,omitempty"`
	TaskExpiresAt    string              `json:"task_expires_at,omitempty"`
	ContextExpiresAt string              `json:"context_expires_at,omitempty"`
	State            string              `json:"state"`
	Parts            []a2aclient.Part    `json:"parts,omitempty"`
	StatusMessages   []a2aResultMessage  `json:"status_messages,omitempty"`
	Artifacts        []a2aResultArtifact `json:"artifacts,omitempty"`
	History          []a2aResultMessage  `json:"history,omitempty"`
	ContentTruncated bool                `json:"content_truncated,omitempty"`
}

// Reserve the largest possible authority envelope before sending. Application
// content is fitted against the same reservation before capability commit.
func a2aEnvelopeReservation() a2aEnvelope {
	return a2aEnvelope{
		Kind: "message", State: "input-required", ContentTruncated: true,
		TaskHandle: strings.Repeat("x", 52), ContextHandle: strings.Repeat("x", 52),
		TaskExpiresAt: "2000-01-01T00:00:00.123456789Z", ContextExpiresAt: "2000-01-01T00:00:00.123456789Z",
	}
}

func a2aResultLimit(limit int) int {
	if limit == 0 {
		return defaultMaxToolResultBytes
	}
	return limit
}

func checkA2AResultBudget(limit int) error {
	body, err := json.Marshal(a2aEnvelopeReservation())
	if err != nil || a2aResultLimit(limit) > 0 && len(body) > a2aResultLimit(limit) {
		return a2aLocalError("result_budget_too_small")
	}
	return nil
}

// prepareA2AEnvelope must complete before capability commit. Only accepted
// application fields are projected; protocol references stay in admission data.
func prepareA2AEnvelope(result a2aclient.Result, limit int) (a2aEnvelope, error) {
	envelope := a2aEnvelopeReservation()
	envelope.Kind, envelope.State, envelope.ContentTruncated = result.Kind, result.State, false
	if result.Kind != "message" && result.Kind != "task" {
		return a2aEnvelope{}, a2aLocalError("invalid_result")
	}
	if result.Kind == "message" {
		envelope.Parts = result.Parts
	} else {
		for _, message := range result.StatusMessages {
			envelope.StatusMessages = append(envelope.StatusMessages, a2aResultMessage{Role: message.Role, Parts: message.Parts})
		}
		for _, artifact := range result.Artifacts {
			envelope.Artifacts = append(envelope.Artifacts, a2aResultArtifact{Name: artifact.Name, Description: artifact.Description, Parts: artifact.Parts})
		}
		for _, message := range result.History {
			envelope.History = append(envelope.History, a2aResultMessage{Role: message.Role, Parts: message.Parts})
		}
	}
	if err := checkA2AResultBudget(limit); err != nil {
		return a2aEnvelope{}, err
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return a2aEnvelope{}, a2aLocalError("invalid_result")
	}
	limit = a2aResultLimit(limit)
	if limit < 0 || len(encoded) <= limit {
		return envelope, nil
	}
	envelope.ContentTruncated = true
	// Binary search a prefix of semantic units. Never clip JSON, text bytes,
	// or artifact/message boundaries. The full result was validated above.
	low, high := 0, len(envelope.Parts)+len(envelope.StatusMessages)+len(envelope.Artifacts)+len(envelope.History)
	for low < high {
		mid := low + (high-low+1)/2
		candidate := envelope.prefix(mid)
		body, err := json.Marshal(candidate)
		if err != nil {
			return a2aEnvelope{}, a2aLocalError("invalid_result")
		}
		if len(body) <= limit {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return envelope.prefix(low), nil
}

func (e a2aEnvelope) prefix(n int) a2aEnvelope {
	count := min(n, len(e.Parts))
	e.Parts, n = e.Parts[:count], n-count
	count = min(n, len(e.StatusMessages))
	e.StatusMessages, n = e.StatusMessages[:count], n-count
	count = min(n, len(e.Artifacts))
	e.Artifacts, n = e.Artifacts[:count], n-count
	e.History = e.History[:min(n, len(e.History))]
	return e
}

// deliver replaces fixed-size reservations with verified authority only. The
// caller must not mutate application content between preparation and delivery.
func (e a2aEnvelope) deliver(delivery capabilityDelivery) (*ToolCallResult, error) {
	e.TaskHandle, e.ContextHandle = delivery.taskHandle, delivery.contextHandle
	e.TaskExpiresAt, e.ContextExpiresAt = "", ""
	if e.TaskHandle != "" {
		e.TaskExpiresAt = delivery.taskExpires.UTC().Format(time.RFC3339Nano)
	}
	if e.ContextHandle != "" {
		e.ContextExpiresAt = delivery.contextExpires.UTC().Format(time.RFC3339Nano)
	}
	encoded, err := json.Marshal(e)
	if err != nil {
		return nil, a2aLocalError("invalid_result")
	}
	return &ToolCallResult{Content: []Content{NewTextContent(string(encoded))}, atomicResult: true}, nil
}

func a2aAdmissionResponse(result a2aclient.Result) capabilityResponse {
	response := capabilityResponse{contextID: result.ContextID, taskID: result.TaskID, state: result.State}
	for _, messages := range [][]a2aclient.Message{result.StatusMessages, result.History} {
		for _, message := range messages {
			response.references = append(response.references, capabilityReference{contextID: message.ContextID, taskID: message.TaskID})
		}
	}
	return response
}
