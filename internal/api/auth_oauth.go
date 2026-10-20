package api

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/resourceauth"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const oauthMetadataRoot = "/.well-known/oauth-protected-resource"

// oauthMetadataPath reports an exact RFC 9728 metadata path. Matching is on
// the cleaned path and allows one group segment. /.well-known/agent.json and
// /.well-known/oauth-protected-resource/extra do not match.
func oauthMetadataPath(cleaned string) bool {
	if cleaned == oauthMetadataRoot || cleaned == oauthMetadataRoot+"/mcp" {
		return true
	}
	const prefix = oauthMetadataRoot + "/groups/"
	const suffix = "/mcp"
	if len(cleaned) <= len(prefix)+len(suffix) || !strings.HasPrefix(cleaned, prefix) || !strings.HasSuffix(cleaned, suffix) {
		return false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(cleaned, prefix), suffix)
	return name != "" && !strings.Contains(name, "/") && name != "." && name != ".."
}

func oauthMetadataRequest(raw string) bool {
	if oauthMetadataPath(raw) || oauthMetadataPath(path.Clean(raw)) {
		return true
	}
	if unesc, err := url.PathUnescape(raw); err == nil {
		return oauthMetadataPath(unesc) || oauthMetadataPath(path.Clean(unesc))
	}
	return false
}

// bearerChallenge builds a WWW-Authenticate Bearer challenge. scope is omitted
// when empty. errorCode is omitted when empty.
func bearerChallenge(metadataURL string, scopes []string, errorCode string) string {
	var b strings.Builder
	b.WriteString("Bearer resource_metadata=")
	b.WriteString(strconv.Quote(metadataURL))
	if errorCode != "" {
		b.WriteString(", error=")
		b.WriteString(strconv.Quote(errorCode))
	}
	if len(scopes) > 0 {
		b.WriteString(", scope=")
		b.WriteString(strconv.Quote(strings.Join(scopes, " ")))
	}
	return b.String()
}

func (s *Server) oauthAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isProtectedPath(r.URL.Path) && !isProtectedPath(path.Clean(r.URL.Path)) {
			next.ServeHTTP(w, r)
			return
		}
		if oauthMetadataRequest(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if s.oauthVerifier == nil {
			writeOAuthUnavailable(w)
			return
		}
		if err := s.oauthVerifier.Ready(r.Context()); err != nil {
			slog.Debug("oauth discovery failed; protected routes answer 503 until the issuer is reachable",
				"issuer_host", issuerHost(s.oauthCfg.Issuer), "category", resourceauth.CategoryDiscoveryUnavailable)
			writeOAuthUnavailable(w)
			return
		}
		raw, ok := bearerFromAuthorization(r)
		if !ok {
			code := ""
			if len(r.Header.Values("Authorization")) != 0 {
				code = "invalid_token"
			}
			s.writeOAuthDenied(w, r, http.StatusUnauthorized, code, "Unauthorized")
			return
		}
		principal, err := s.oauthVerifier.Verify(r.Context(), raw)
		if err != nil {
			s.writeOAuthVerifyError(w, r, err)
			return
		}
		ctx := mcp.WithPrincipal(r.Context(), principal.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) writeOAuthVerifyError(w http.ResponseWriter, r *http.Request, err error) {
	var scopeErr *resourceauth.ScopeError
	if errors.As(err, &scopeErr) {
		slog.Debug("oauth token rejected", "category", resourceauth.CategoryScope)
		s.writeOAuthDenied(w, r, http.StatusForbidden, "insufficient_scope", "Forbidden")
		return
	}
	if errors.Is(err, resourceauth.ErrUnavailable) {
		cat := resourceauth.CategoryDiscoveryUnavailable
		var disc *resourceauth.DiscoveryError
		if errors.As(err, &disc) && disc.Category != "" {
			cat = disc.Category
		}
		slog.Debug("oauth discovery failed; protected routes answer 503 until the issuer is reachable",
			"issuer_host", issuerHost(s.oauthCfg.Issuer), "category", cat)
		writeOAuthUnavailable(w)
		return
	}
	cat := resourceauth.CategoryMalformed
	var tok *resourceauth.TokenError
	if errors.As(err, &tok) && tok.Category != "" {
		cat = tok.Category
	}
	slog.Debug("oauth token rejected", "category", cat)
	s.writeOAuthDenied(w, r, http.StatusUnauthorized, "invalid_token", "Unauthorized")
}

func (s *Server) writeOAuthDenied(w http.ResponseWriter, r *http.Request, status int, errorCode, body string) {
	w.Header().Set("WWW-Authenticate", bearerChallenge(s.oauthMetadataURL(r), s.oauthCfg.RequiredScopes, errorCode))
	w.Header().Set("Gridctl-Auth-Rejected", "1")
	http.Error(w, body, status)
}

func writeOAuthUnavailable(w http.ResponseWriter) {
	http.Error(w, "authorization server unavailable", http.StatusServiceUnavailable)
}

func bearerFromAuthorization(r *http.Request) (string, bool) {
	if len(r.Header.Values("Authorization")) != 1 {
		return "", false
	}
	val := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(val, prefix) {
		return "", false
	}
	raw := strings.TrimSpace(val[len(prefix):])
	if raw == "" {
		return "", false
	}
	return raw, true
}

func (s *Server) oauthMetadataURL(r *http.Request) string {
	base := s.oauthPublicBase(r)
	if group := oauthChallengeGroup(r.URL.Path); group != "" {
		return base + oauthMetadataRoot + "/groups/" + url.PathEscape(group) + "/mcp"
	}
	return base + oauthMetadataRoot
}

func (s *Server) oauthPublicBase(r *http.Request) string {
	if s.cardPublicURL != "" {
		return strings.TrimRight(s.cardPublicURL, "/")
	}
	host := r.Host
	if host == "" {
		host = "localhost"
	}
	return "http://" + host
}

func (s *Server) oauthResource(r *http.Request, group string) string {
	if u, err := url.Parse(s.oauthCfg.Audience); err == nil && u.IsAbs() && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") {
		return s.oauthCfg.Audience
	}
	base := s.oauthPublicBase(r)
	if group != "" {
		return base + "/groups/" + url.PathEscape(group) + "/mcp"
	}
	return base + "/mcp"
}

func oauthChallengeGroup(raw string) string {
	candidates := []string{raw, path.Clean(raw)}
	if unesc, err := url.PathUnescape(raw); err == nil {
		candidates = append(candidates, unesc, path.Clean(unesc))
	}
	for _, c := range candidates {
		parts := strings.Split(path.Clean(c), "/")
		if len(parts) == 4 && parts[1] == "groups" && (parts[3] == "mcp" || parts[3] == "sse") && parts[2] != "" && parts[2] != "." && parts[2] != ".." {
			return parts[2]
		}
	}
	return ""
}

func issuerHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func (s *Server) handleOAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, OPTIONS")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	group := r.PathValue("name")
	if strings.Contains(path.Clean(r.URL.Path), "/groups/") {
		if group == "" || s.gateway == nil || !s.gateway.HasGroup(group) {
			http.NotFound(w, r)
			return
		}
	}
	doc := oauthex.ProtectedResourceMetadata{
		Resource:               s.oauthResource(r, group),
		AuthorizationServers:   []string{s.oauthCfg.Issuer},
		BearerMethodsSupported: []string{"header"},
	}
	if len(s.oauthCfg.RequiredScopes) > 0 {
		doc.ScopesSupported = append([]string(nil), s.oauthCfg.RequiredScopes...)
	}
	auth.ProtectedResourceMetadataHandler(&doc).ServeHTTP(w, r)
}

func (s *Server) oauthPublicMetadata(raw string) bool {
	return s != nil && s.oauthEnabled && oauthMetadataRequest(raw)
}

// Metadata preflights terminate in CORS before the ordinary Host middleware.
func (s *Server) oauthMetadataPreflightHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions && s.oauthPublicMetadata(r.URL.Path) {
			if err := mcp.ValidateRequestHost(r, s.allowedHosts); err != nil {
				http.Error(w, "Forbidden: "+err.Error(), http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
