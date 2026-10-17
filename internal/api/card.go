package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gridctl/gridctl/internal/probe"
	"github.com/gridctl/gridctl/pkg/servercard"
)

type cardDetectRequest struct {
	URL string `json:"url"`
}

type cardAuthDraft struct {
	Type   string `json:"type"`
	Token  string `json:"token,omitempty"`
	Header string `json:"header,omitempty"`
	Value  string `json:"value,omitempty"`
}

type cardServerDraft struct {
	Name               string         `json:"name"`
	URL                string         `json:"url"`
	Transport          string         `json:"transport"`
	Auth               *cardAuthDraft `json:"auth,omitempty"`
	ProtocolGeneration string         `json:"protocol_generation,omitempty"`
}

type cardInputDraft struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
}

type cardDetectResponse struct {
	Card     *servercard.ServerCard `json:"card"`
	Server   cardServerDraft        `json:"server"`
	Warnings []string               `json:"warnings,omitempty"`
	Inputs   []cardInputDraft       `json:"inputs,omitempty"`
}

func (s *Server) handleServerCardDetect(w http.ResponseWriter, r *http.Request) {
	if s.probeLimiter == nil {
		s.probeLimiter = newProbeLimiter()
	}
	session := r.Header.Get(sessionHeader)
	if session == "" {
		session = r.RemoteAddr
	}
	release, sessionLimited, globalLimited := s.probeLimiter.acquire(session)
	if sessionLimited || globalLimited {
		w.Header().Set("Retry-After", "3")
		code := http.StatusTooManyRequests
		msg := "Too many probes in progress for this session."
		if globalLimited {
			code = http.StatusServiceUnavailable
			msg = "Probe service is at capacity. Try again in a few seconds."
		}
		writeProbeError(w, code, "rate_limited", msg, "")
		return
	}
	defer release()

	body, err := io.ReadAll(io.LimitReader(r.Body, probeRequestMaxBytes+1))
	if err != nil {
		writeProbeError(w, http.StatusBadRequest, probe.CodeInvalidConfig, "Failed to read request body.", "")
		return
	}
	if len(body) > probeRequestMaxBytes {
		writeProbeError(w, http.StatusRequestEntityTooLarge, probe.CodeInvalidConfig, "Request body exceeds 64 KiB.", "")
		return
	}
	var req cardDetectRequest
	if err := json.Unmarshal(body, &req); err != nil || req.URL == "" {
		writeProbeError(w, http.StatusBadRequest, probe.CodeInvalidConfig, "Request must be JSON with a url.", "")
		return
	}
	card, err := servercard.Resolve(r.Context(), req.URL, servercard.FetchOptions{})
	if err != nil {
		writeCardError(w, err)
		return
	}
	draft, err := servercard.DraftFromCard(card, req.URL)
	if err != nil {
		writeCardError(w, err)
		return
	}
	server, warnings, err := draft.ServerDraft("")
	if err != nil {
		writeCardError(w, err)
		return
	}
	warnings = append(draft.Warnings, warnings...)
	resp := cardDetectResponse{
		Card: card,
		Server: cardServerDraft{
			Name:               server.Name,
			URL:                server.URL,
			Transport:          server.Transport,
			ProtocolGeneration: server.ProtocolGeneration,
		},
		Warnings: warnings,
	}
	if server.Auth != nil {
		resp.Server.Auth = &cardAuthDraft{
			Type:   server.Auth.Type,
			Token:  server.Auth.Token,
			Header: server.Auth.Header,
			Value:  server.Auth.Value,
		}
	}
	for _, in := range draft.Entry.Inputs {
		resp.Inputs = append(resp.Inputs, cardInputDraft{
			Name:        in.Name,
			Description: in.Description,
			Required:    in.Required,
			Secret:      in.Secret,
		})
	}
	writeJSON(w, resp)
}

func writeCardError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, servercard.ErrNoCard):
		writeProbeError(w, http.StatusNotFound, "no_card", "No server card was found at that URL.", "")
	case errors.Is(err, servercard.ErrInvalidCard):
		writeProbeError(w, http.StatusUnprocessableEntity, "invalid_card", "The server card was not valid.", "")
	case errors.Is(err, servercard.ErrOffOrigin):
		writeProbeError(w, http.StatusUnprocessableEntity, "off_origin", "The server card remote is not the same origin as the submitted URL.", "")
	case errors.Is(err, servercard.ErrTemplateURL):
		writeProbeError(w, http.StatusUnprocessableEntity, "off_origin", "The server card remote is a URL template and was refused.", "")
	default:
		writeProbeError(w, http.StatusBadGateway, "fetch_failed", "The server card could not be fetched.", "")
	}
}
