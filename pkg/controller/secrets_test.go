package controller

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestApplyGatewayAuth_UsesResolvedToken(t *testing.T) {
	literal := &config.Stack{
		Name: "demo",
		Gateway: &config.GatewayConfig{Auth: &config.AuthConfig{
			Type:  "bearer",
			Token: "${secret:fake/gw}",
		}},
	}
	resolved := literal.Clone()
	config.SubstituteSecretRefs(resolved, map[string]string{"secret:fake/gw": "resolved-token"})
	if resolved.Gateway.Auth.Token != "resolved-token" {
		t.Fatalf("resolved token = %q", resolved.Gateway.Auth.Token)
	}
	b := &GatewayBuilder{stack: literal}
	b.SetResolved(resolved, map[string]string{"secret:fake/gw": "resolved-token"}, nil)

	var gotType, gotToken, gotHeader string
	applyGatewayAuth(b.listenerStack(), func(authType, token, header string) {
		gotType, gotToken, gotHeader = authType, token, header
	})
	if gotType != "bearer" || gotToken != "resolved-token" || gotHeader != "" {
		t.Fatalf("SetAuth saw %q %q %q", gotType, gotToken, gotHeader)
	}

	// The applied-config hook assigns only the literal stack.
	b.stack = literal
	if b.stack.Gateway.Auth.Token != "${secret:fake/gw}" {
		t.Fatal("hook target was resolved")
	}
	if b.listenerStack().Gateway.Auth.Token != "resolved-token" {
		t.Fatal("listener stack followed the literal hook assignment")
	}
}
