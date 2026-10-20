package resourceauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	expiryLeeway = 30 * time.Second
	nbfLeeway    = 5 * time.Minute
)

func checkTyp(raw string) error {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return invalidToken(CategoryMalformed)
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		headerJSON, err = base64.URLEncoding.DecodeString(parts[0])
		if err != nil {
			return invalidToken(CategoryMalformed)
		}
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return invalidToken(CategoryMalformed)
	}
	if header.Alg == "" || header.Alg == "none" || strings.HasPrefix(header.Alg, "HS") {
		return invalidToken(CategoryAlgorithm)
	}
	switch strings.ToLower(header.Typ) {
	case "", "jwt", "at+jwt", "application/at+jwt":
		return nil
	default:
		return invalidToken(CategoryTyp)
	}
}

func principalFromToken(cfg Config, idToken *oidc.IDToken) (*Principal, error) {
	if idToken == nil {
		return nil, invalidToken(CategoryMalformed)
	}
	now := time.Now()
	if idToken.Expiry.IsZero() || now.Sub(idToken.Expiry) > expiryLeeway {
		return nil, invalidToken(CategoryExpired)
	}
	var raw map[string]json.RawMessage
	if err := idToken.Claims(&raw); err != nil {
		return nil, invalidToken(CategoryMalformed)
	}
	if nbf, ok, err := numericDate(raw["nbf"]); err != nil {
		return nil, invalidToken(CategoryMalformed)
	} else if ok && nbf.Sub(now) > nbfLeeway {
		return nil, invalidToken(CategoryNotYetValid)
	}
	claim := cfg.IdentityClaim
	if claim == "" {
		claim = "sub"
	}
	id, err := stringClaim(raw[claim])
	if err != nil || id == "" {
		return nil, invalidToken(CategoryIdentity)
	}
	scopes := unionScopes(raw["scope"], raw["scp"])
	if missing := missingScopes(cfg.RequiredScopes, scopes); len(missing) > 0 {
		return nil, &ScopeError{Missing: missing, Required: append([]string(nil), cfg.RequiredScopes...)}
	}
	return &Principal{ID: id, Scopes: scopes, Expiry: idToken.Expiry}, nil
}

func stringClaim(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("missing")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

func numericDate(raw json.RawMessage) (time.Time, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, false, nil
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return time.Time{}, false, err
	}
	if n >= math.MaxInt64 || n <= math.MinInt64 {
		return time.Time{}, false, errors.New("numeric date out of range")
	}
	sec := int64(n)
	nsec := int64((n - float64(sec)) * float64(time.Second))
	return time.Unix(sec, nsec), true, nil
}

func unionScopes(scopeRaw, scpRaw json.RawMessage) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	if s, err := stringClaim(scopeRaw); err == nil {
		for _, part := range strings.Fields(s) {
			add(part)
		}
	}
	if len(scpRaw) == 0 {
		return out
	}
	if s, err := stringClaim(scpRaw); err == nil {
		for _, part := range strings.Fields(s) {
			add(part)
		}
		return out
	}
	var arr []string
	if json.Unmarshal(scpRaw, &arr) == nil {
		for _, part := range arr {
			add(part)
		}
	}
	return out
}

func missingScopes(required, have []string) []string {
	if len(required) == 0 {
		return nil
	}
	set := map[string]bool{}
	for _, s := range have {
		set[s] = true
	}
	var missing []string
	for _, s := range required {
		if !set[s] {
			missing = append(missing, s)
		}
	}
	return missing
}

func classifyVerifyError(err error, identityClaim string) string {
	if err == nil {
		return CategoryMalformed
	}
	var expired *oidc.TokenExpiredError
	if errors.As(err, &expired) {
		return CategoryExpired
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unsupported algorithm"):
		return CategoryAlgorithm
	case strings.Contains(msg, "audience"):
		return CategoryAudience
	case strings.Contains(msg, "issuer") || strings.Contains(msg, "different provider"):
		return CategoryIssuer
	case identityClaim != "" && strings.Contains(msg, "cannot unmarshal") && strings.Contains(msg, strings.ToLower(identityClaim)):
		return CategoryIdentity
	case strings.Contains(msg, "key id") || strings.Contains(msg, "unknown kid"):
		return CategoryUnknownKID
	case strings.Contains(msg, "signature") || strings.Contains(msg, "verification error") || strings.Contains(msg, "crypto/rsa"):
		return CategorySignature
	default:
		return CategoryMalformed
	}
}

func headerKid(raw string) string {
	parts := strings.Split(raw, ".")
	if len(parts) < 1 {
		return ""
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ""
	}
	var header struct {
		Kid string `json:"kid"`
	}
	if json.Unmarshal(headerJSON, &header) != nil {
		return ""
	}
	return header.Kid
}

func isKeyFetchFailure(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "fetching keys") || strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host")
}
