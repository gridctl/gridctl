package config

import (
	"errors"
	"time"

	"github.com/gridctl/gridctl/pkg/a2aclient"
	"github.com/gridctl/gridctl/pkg/flags"
	"gopkg.in/yaml.v3"
)

// A2AConfig declares an experimental outbound Agent Card source.
type A2AConfig struct {
	Card     string   `yaml:"card"`
	Endpoint string   `yaml:"endpoint,omitempty"`
	Dialect  string   `yaml:"dialect,omitempty"`
	Profile  string   `yaml:"profile,omitempty"`
	Include  []string `yaml:"include,omitempty"`
	Timeout  string   `yaml:"timeout,omitempty"`
	Auth     *A2AAuth `yaml:"auth,omitempty"`
}

// A2AAuth supplies operator-provisioned bearer authentication.
type A2AAuth struct {
	Type  string `yaml:"type"`
	Token string `yaml:"token"`
}

// UnmarshalYAML refuses caller-selected sessions, including empty overrides.
func (a *A2AAuth) UnmarshalYAML(node *yaml.Node) error {
	var fields map[string]yaml.Node
	if err := node.Decode(&fields); err != nil {
		return errors.New("a2a.auth must be a mapping")
	}
	if _, present := fields["session_id"]; present {
		return errors.New("a2a.auth.session_id is unsupported; sessions are generated")
	}
	type plain A2AAuth
	return node.Decode((*plain)(a))
}

// IsA2A reports whether this is an outbound A2A source without another source.
func (s *MCPServer) IsA2A() bool {
	return s.A2A != nil && s.Image == "" && s.Source == nil && s.URL == "" && len(s.Command) == 0 && s.SSH == nil && s.OpenAPI == nil
}

func validateA2A(s *Stack, server MCPServer, prefix string) ValidationErrors {
	var errs ValidationErrors
	add := func(field, message string) { errs = append(errs, ValidationError{prefix + field, message}) }
	if !flags.Resolve(flags.Default(), s.Experimental).Enabled["a2a"] {
		add(".a2a", "requires experimental.a2a (or GRIDCTL_EXPERIMENTAL_A2A)")
	}
	a := server.A2A
	if _, err := a2aclient.ParseURL(a.Card); err != nil {
		add(".a2a.card", err.Error())
	}
	if a.Endpoint != "" {
		if _, err := a2aclient.ParseURL(a.Endpoint); err != nil {
			add(".a2a.endpoint", err.Error())
		}
	}
	if a.Dialect != "" && a.Dialect != "auto" && a.Dialect != "1.0" && a.Dialect != "0.3" {
		add(".a2a.dialect", "must be auto, 1.0, or 0.3")
	}
	if a.Profile != "" && a.Profile != "bedrock" {
		add(".a2a.profile", "must be empty or bedrock")
	}
	if a.Timeout != "" {
		if d, err := time.ParseDuration(a.Timeout); err != nil || d <= 0 {
			add(".a2a.timeout", "must be a positive duration")
		}
	}
	if a.Auth != nil {
		if a.Auth.Type != "bearer" {
			add(".a2a.auth.type", "must be bearer")
		}
		if a.Auth.Token == "" {
			add(".a2a.auth.token", "is required")
		}
	}
	if server.Transport != "" {
		add(".transport", "not applicable for A2A servers")
	}
	if server.Port != 0 {
		add(".port", "not applicable for A2A servers")
	}
	if server.Network != "" {
		add(".network", "not applicable for A2A servers")
	}
	return errs
}
