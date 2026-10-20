package resourceauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const (
	discoveryTimeout = 10 * time.Second
	maxBackoff       = 60 * time.Second
)

// defaultSigningAlgs is used when discovery is skipped or the document does
// not advertise id_token_signing_alg_values_supported. The list is never
// empty: go-oidc treats an empty SupportedSigningAlgs as RS256 only.
var defaultSigningAlgs = []string{"RS256", "ES256", "ES384", "ES512", "PS256", "EdDSA"}

// asymmetricAlgs are the signature algorithms this verifier will accept from
// an advertised list. none and HMAC are never included.
var asymmetricAlgs = map[string]bool{
	"RS256": true, "RS384": true, "RS512": true,
	"ES256": true, "ES384": true, "ES512": true,
	"PS256": true, "PS384": true, "PS512": true,
	"EdDSA": true,
}

type discoveryDoc struct {
	issuer string
	jwks   string
	algs   []string
}

func (v *Verifier) discoverLocked(ctx context.Context) error {
	dctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	doc, err := v.loadDiscovery(dctx)
	if err != nil {
		return err
	}
	algs := filterSigningAlgs(doc.algs)
	if len(algs) == 0 {
		algs = append([]string(nil), defaultSigningAlgs...)
	}
	jwks := doc.jwks
	if v.cfg.JWKSURL != "" {
		jwks = v.cfg.JWKSURL
	}
	if jwks == "" {
		return discoveryUnavailable(fmt.Errorf("discovery document has no jwks_uri"))
	}
	keyCtx := v.keyCtx
	if keyCtx == nil {
		keyCtx = context.Background()
	}
	keySet := oidcRemoteKeySet(keyCtx, jwks)
	v.keySet = keySet
	v.verifier = newOIDCVerifier(v.cfg.Issuer, keySet, v.cfg.Audience, algs)
	v.algs = algs
	return nil
}

func (v *Verifier) loadDiscovery(ctx context.Context) (discoveryDoc, error) {
	if v.cfg.JWKSURL != "" {
		return discoveryDoc{issuer: v.cfg.Issuer, jwks: v.cfg.JWKSURL, algs: nil}, nil
	}
	meta, err := auth.GetAuthServerMetadata(ctx, v.cfg.Issuer, v.client)
	if err != nil {
		return discoveryDoc{}, discoveryUnavailable(err)
	}
	if meta == nil {
		return discoveryDoc{}, discoveryUnavailable(fmt.Errorf("discovery_unavailable"))
	}
	if meta.Issuer != v.cfg.Issuer {
		return discoveryDoc{}, discoveryUnavailable(fmt.Errorf("discovered issuer mismatch"))
	}
	algs, err := v.signingAlgs(ctx, meta)
	if err != nil {
		return discoveryDoc{}, err
	}
	return discoveryDoc{issuer: meta.Issuer, jwks: meta.JWKSURI, algs: algs}, nil
}

func (v *Verifier) signingAlgs(ctx context.Context, meta *oauthex.AuthServerMeta) ([]string, error) {
	for _, metadataURL := range authorizationServerMetadataURLs(v.cfg.Issuer) {
		body, status, err := v.getMetadata(ctx, metadataURL)
		if err != nil {
			return nil, discoveryUnavailable(err)
		}
		if status == http.StatusNotFound || len(body) == 0 {
			continue
		}
		if status != http.StatusOK {
			return nil, discoveryUnavailable(fmt.Errorf("metadata status %d", status))
		}
		var raw struct {
			Issuer             string   `json:"issuer"`
			JWKSURI            string   `json:"jwks_uri"`
			IDTokenSigningAlgs []string `json:"id_token_signing_alg_values_supported"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, discoveryUnavailable(err)
		}
		if raw.Issuer != meta.Issuer || raw.JWKSURI != meta.JWKSURI {
			return nil, discoveryUnavailable(fmt.Errorf("discovery document changed during retrieval"))
		}
		return raw.IDTokenSigningAlgs, nil
	}
	return nil, nil
}

func (v *Verifier) getMetadata(ctx context.Context, metadataURL string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// authorizationServerMetadataURLs follows the MCP discovery order: RFC 8414
// path insertion, then OIDC path insertion, then OIDC path appending.
func authorizationServerMetadataURLs(issuer string) []string {
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil
	}
	origin := u.Scheme + "://" + u.Host
	p := strings.TrimSuffix(u.Path, "/")
	if p != "" {
		return []string{
			origin + "/.well-known/oauth-authorization-server" + p,
			origin + "/.well-known/openid-configuration" + p,
			strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration",
		}
	}
	return []string{
		origin + "/.well-known/oauth-authorization-server",
		origin + "/.well-known/openid-configuration",
	}
}

func filterSigningAlgs(advertised []string) []string {
	if len(advertised) == 0 {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, alg := range advertised {
		if !asymmetricAlgs[alg] || seen[alg] {
			continue
		}
		seen[alg] = true
		out = append(out, alg)
	}
	return out
}
