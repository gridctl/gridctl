package api

import (
	"net/http"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/reload"
)

type secretsResponse struct {
	Providers  []secretProviderDTO  `json:"providers"`
	References []secretReferenceDTO `json:"references"`
}

type secretProviderDTO struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Command   []string `json:"command"`
	Timeout   string   `json:"timeout"`
	TokenFile bool     `json:"token_file"`
}

type secretReferenceDTO struct {
	Key          string            `json:"key"`
	Provider     string            `json:"provider"`
	Path         string            `json:"path"`
	Consumers    []config.Consumer `json:"consumers"`
	Status       string            `json:"status"`
	Error        *string           `json:"error"`
	LastResolved *string           `json:"last_resolved"`
}

func (s *Server) handleSecrets(w http.ResponseWriter, _ *http.Request) {
	handler := s.ReloadHandler()
	if handler == nil || handler.CurrentConfig() == nil {
		writeJSON(w, emptySecrets())
		return
	}
	writeJSON(w, buildSecretsResponse(handler.CurrentConfig(), handler.SecretObservation()))
}

func emptySecrets() secretsResponse {
	return secretsResponse{Providers: []secretProviderDTO{}, References: []secretReferenceDTO{}}
}

func buildSecretsResponse(spec *config.Stack, obs reload.SecretObservation) secretsResponse {
	out := emptySecrets()
	if spec == nil {
		return out
	}
	if spec.Secrets != nil {
		for _, provider := range spec.Secrets.Providers {
			timeout := provider.Timeout
			if timeout == "" {
				timeout = "10s"
			}
			var command []string
			if len(provider.Command) > 0 {
				command = append([]string(nil), provider.Command...)
			}
			out.Providers = append(out.Providers, secretProviderDTO{
				Name:      provider.Name,
				Type:      provider.Type,
				Command:   command,
				Timeout:   timeout,
				TokenFile: provider.TokenFile != "",
			})
		}
	}
	for _, ref := range spec.SecretRefs {
		consumers := append([]config.Consumer(nil), spec.References[ref.Key]...)
		if consumers == nil {
			consumers = []config.Consumer{}
		}
		dto := secretReferenceDTO{
			Key:       ref.Key,
			Provider:  ref.Provider,
			Path:      ref.Path,
			Consumers: consumers,
			Status:    "pending",
		}
		if _, ok := obs.Resolved[ref.Key]; ok {
			dto.Status = "resolved"
			if at, ok := obs.ResolvedAt[ref.Key]; ok && !at.IsZero() {
				text := at.UTC().Format(time.RFC3339)
				dto.LastResolved = &text
			}
		} else if msg, failed := referenceError(ref, consumers, obs); failed {
			dto.Status = "error"
			dto.Error = &msg
		}
		out.References = append(out.References, dto)
	}
	return out
}

func referenceError(ref config.SecretRef, consumers []config.Consumer, obs reload.SecretObservation) (string, bool) {
	for _, consumer := range consumers {
		if consumer.Kind != config.RefKindMCPServer && consumer.Kind != config.RefKindResource {
			continue
		}
		err, ok := obs.Unresolved[consumer.Name]
		if ok && err != nil {
			return err.Error(), true
		}
	}
	if err, ok := obs.Unresolved[ref.Provider]; ok && err != nil {
		return err.Error(), true
	}
	return "", false
}
