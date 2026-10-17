package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/servercard"
)

func (s *Server) handleServerCard(w http.ResponseWriter, r *http.Request) {
	s.writeServerCard(w, r, "")
}

func (s *Server) handleGroupServerCard(w http.ResponseWriter, r *http.Request) {
	name := s.configuredGroupName(r.PathValue("name"))
	if name == "" {
		http.NotFound(w, r)
		return
	}
	s.writeServerCard(w, r, name)
}

func (s *Server) handleAICatalog(w http.ResponseWriter, r *http.Request) {
	origin, prefix, fromRequest := s.cardOrigin(r)
	body, err := json.Marshal(s.buildCatalog(origin, prefix))
	if err != nil {
		http.Error(w, "catalog unavailable", http.StatusInternalServerError)
		return
	}
	writeDiscovery(w, r, servercard.MediaTypeCatalog, body, fromRequest)
}

func (s *Server) writeServerCard(w http.ResponseWriter, r *http.Request, group string) {
	origin, prefix, fromRequest := s.cardOrigin(r)
	card, err := s.buildCard(group, origin, prefix)
	if err != nil {
		http.Error(w, "server card unavailable", http.StatusInternalServerError)
		return
	}
	body, err := json.Marshal(card)
	if err != nil {
		http.Error(w, "server card unavailable", http.StatusInternalServerError)
		return
	}
	writeDiscovery(w, r, servercard.MediaTypeServerCard, body, fromRequest)
}

func writeDiscovery(w http.ResponseWriter, r *http.Request, media string, body []byte, varyHost bool) {
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("Content-Type", media)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", etag)
	if varyHost {
		w.Header().Set("Vary", "Host")
	}
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || part == etag {
			return true
		}
	}
	return false
}

func (s *Server) buildCard(group, origin, prefix string) (*servercard.ServerCard, error) {
	name, version := "gridctl-gateway", "dev"
	if s.gateway != nil {
		info := s.gateway.ServerInfo()
		if info.Name != "" {
			name = info.Name
		}
		if info.Version != "" {
			version = info.Version
		}
	}
	return servercard.Build(servercard.Identity{
		GatewayName:      name,
		StackName:        s.stackName,
		Description:      s.cardDescription,
		Version:          version,
		Group:            group,
		AuthType:         s.authType,
		AuthHeader:       s.authHeader,
		Origin:           origin,
		PathPrefix:       prefix,
		ProtocolVersions: append([]string(nil), mcp.SupportedProtocolVersions...),
	})
}

func (s *Server) buildCatalog(origin, prefix string) *servercard.Catalog {
	name := "gridctl-gateway"
	if s.gateway != nil && s.gateway.ServerInfo().Name != "" {
		name = s.gateway.ServerInfo().Name
	}
	segment := sanitizedGatewaySegment(name)
	cards := map[string]string{}
	var groups []string
	if origin != "" {
		cards[""] = servercard.CardURL(origin, prefix, "")
		if s.gateway != nil {
			groups = s.gateway.CurrentGroupPolicy().Names()
		}
		for _, group := range groups {
			cards[group] = servercard.CardURL(origin, prefix, group)
		}
	}
	return servercard.BuildCatalog(origin, prefix, segment, groups, cards)
}

func sanitizedGatewaySegment(name string) string {
	card, err := servercard.Build(servercard.Identity{GatewayName: name, StackName: "gridctl", Version: "dev"})
	if err != nil {
		return "gateway"
	}
	_, segment, _ := strings.Cut(card.Name, "/")
	if i := strings.Index(segment, "."); i >= 0 {
		segment = segment[:i]
	}
	if segment == "" {
		return "gateway"
	}
	return segment
}

// cardOrigin returns the advertised origin, path prefix, and whether the
// body depends on the request Host. An untrusted Host yields an empty origin.
func (s *Server) cardOrigin(r *http.Request) (origin, prefix string, fromRequest bool) {
	if s.cardPublicURL != "" {
		base, pathPrefix, ok := splitPublicURL(s.cardPublicURL)
		if ok {
			return base, pathPrefix, false
		}
	}
	if r == nil {
		return "", "", false
	}
	host := r.Host
	if host == "" {
		return "", "", false
	}
	local := mcp.RequestLocalAddr(r)
	arrivedLoopback := local == nil || mcp.AddrIsLoopback(local)
	hostname := requestHostname(host)
	if hostname == "" {
		return "", "", false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if arrivedLoopback && mcp.HostIsLoopback(hostname) {
		return scheme + "://" + host, "", true
	}
	if mcp.HostInAllowlist(host, s.allowedHosts) {
		if r.TLS == nil && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		return scheme + "://" + host, "", true
	}
	return "", "", false
}

func splitPublicURL(raw string) (origin, prefix string, ok bool) {
	const (
		httpPrefix  = "http://"
		httpsPrefix = "https://"
	)
	var rest string
	switch {
	case strings.HasPrefix(raw, httpsPrefix):
		rest = strings.TrimPrefix(raw, httpsPrefix)
		origin = httpsPrefix
	case strings.HasPrefix(raw, httpPrefix):
		rest = strings.TrimPrefix(raw, httpPrefix)
		origin = httpPrefix
	default:
		return "", "", false
	}
	host, path, _ := strings.Cut(rest, "/")
	if host == "" || strings.Contains(host, "@") {
		return "", "", false
	}
	origin += host
	if path != "" {
		prefix = "/" + strings.TrimSuffix(path, "/")
	}
	return origin, prefix, true
}

func requestHostname(host string) string {
	if strings.HasPrefix(host, "[") {
		if j := strings.Index(host, "]"); j >= 0 {
			return host[1:j]
		}
		return ""
	}
	name, _, _ := strings.Cut(host, ":")
	return name
}
