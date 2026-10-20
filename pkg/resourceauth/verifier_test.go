package resourceauth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

type testKey struct {
	kid  string
	alg  jose.SignatureAlgorithm
	priv crypto.Signer
	pub  crypto.PublicKey
}

type testIssuer struct {
	srv         *httptest.Server
	mu          sync.Mutex
	keys        []testKey
	signingAlgs []string
	omitAlgs    bool
	issuerURL   string
	overrideIss string
	jwksFetches atomic.Int32
	metaFetches atomic.Int32
	allNotFound atomic.Bool
	jwksDelay   atomic.Int64
}

func newTestIssuer(t *testing.T, keys ...testKey) *testIssuer {
	t.Helper()
	iss := &testIssuer{keys: keys}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", iss.serveMeta)
	mux.HandleFunc("/.well-known/oauth-authorization-server", iss.serveMeta)
	mux.HandleFunc("/jwks", iss.serveJWKS)
	iss.srv = httptest.NewServer(mux)
	iss.issuerURL = iss.srv.URL
	t.Cleanup(iss.srv.Close)
	return iss
}

func (iss *testIssuer) serveMeta(w http.ResponseWriter, r *http.Request) {
	iss.metaFetches.Add(1)
	if iss.allNotFound.Load() {
		http.NotFound(w, r)
		return
	}
	iss.mu.Lock()
	defer iss.mu.Unlock()
	issuer := iss.issuerURL
	if iss.overrideIss != "" {
		issuer = iss.overrideIss
	}
	doc := map[string]any{
		"issuer":                           issuer,
		"authorization_endpoint":           iss.issuerURL + "/auth",
		"token_endpoint":                   iss.issuerURL + "/token",
		"jwks_uri":                         iss.issuerURL + "/jwks",
		"response_types_supported":         []string{"code"},
		"subject_types_supported":          []string{"public"},
		"code_challenge_methods_supported": []string{"S256"},
	}
	if !iss.omitAlgs {
		algs := iss.signingAlgs
		if len(algs) == 0 && len(iss.keys) > 0 {
			algs = []string{string(iss.keys[0].alg)}
		}
		if len(algs) > 0 {
			doc["id_token_signing_alg_values_supported"] = algs
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

func (iss *testIssuer) serveJWKS(w http.ResponseWriter, r *http.Request) {
	iss.jwksFetches.Add(1)
	if d := iss.jwksDelay.Load(); d > 0 {
		time.Sleep(time.Duration(d))
	}
	iss.mu.Lock()
	defer iss.mu.Unlock()
	set := jose.JSONWebKeySet{}
	for _, k := range iss.keys {
		set.Keys = append(set.Keys, jose.JSONWebKey{
			Key:       k.pub,
			KeyID:     k.kid,
			Algorithm: string(k.alg),
			Use:       "sig",
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(set)
}

func (iss *testIssuer) setKeys(keys ...testKey) {
	iss.mu.Lock()
	defer iss.mu.Unlock()
	iss.keys = keys
}

func rsaKey(t *testing.T, kid string) testKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return testKey{kid: kid, alg: jose.RS256, priv: priv, pub: &priv.PublicKey}
}

func ecKey(t *testing.T, kid string) testKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return testKey{kid: kid, alg: jose.ES256, priv: priv, pub: &priv.PublicKey}
}

type tokenSpec struct {
	iss    string
	aud    any
	exp    time.Time
	nbf    *time.Time
	kid    string
	alg    jose.SignatureAlgorithm
	typ    string
	scope  any
	scp    any
	sub    any
	extra  map[string]any
	opaque bool
	raw    string
}

func (iss *testIssuer) mint(t *testing.T, key testKey, spec tokenSpec) string {
	t.Helper()
	if spec.opaque {
		return "not-a-jwt"
	}
	if spec.raw != "" {
		return spec.raw
	}
	claims := map[string]any{
		"iss": iss.issuerURL,
		"sub": "user-1",
		"aud": "gridctl",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	if spec.iss != "" {
		claims["iss"] = spec.iss
	}
	if spec.aud != nil {
		claims["aud"] = spec.aud
	}
	if !spec.exp.IsZero() {
		claims["exp"] = spec.exp.Unix()
	}
	if spec.nbf != nil {
		claims["nbf"] = spec.nbf.Unix()
	}
	if spec.sub != nil {
		claims["sub"] = spec.sub
	}
	if spec.scope != nil {
		claims["scope"] = spec.scope
	}
	if spec.scp != nil {
		claims["scp"] = spec.scp
	}
	for k, v := range spec.extra {
		claims[k] = v
	}
	alg := key.alg
	if spec.alg != "" {
		alg = spec.alg
	}
	kid := key.kid
	if spec.kid != "" {
		kid = spec.kid
	}
	opts := (&jose.SignerOptions{}).WithHeader("kid", kid)
	typ := spec.typ
	if typ == "" {
		typ = "at+jwt"
	}
	if typ != "-" {
		opts = opts.WithType(jose.ContentType(typ))
	}
	var signer jose.Signer
	var err error
	if alg == jose.SignatureAlgorithm("none") {
		return unsignedToken(t, claims, kid, spec.typ)
	}
	if alg == jose.HS256 {
		signer, err = jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: []byte("0123456789abcdef0123456789abcdef")}, opts)
	} else {
		signer, err = jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: key.priv}, opts)
	}
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return compact
}

func unsignedToken(t *testing.T, claims map[string]any, kid, typ string) string {
	t.Helper()
	if typ == "" {
		typ = "JWT"
	}
	header := map[string]string{"alg": "none", "kid": kid, "typ": typ}
	hb, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(pb) + "."
}

func newVerifierFor(t *testing.T, iss *testIssuer, cfg Config) *Verifier {
	t.Helper()
	if cfg.Issuer == "" {
		cfg.Issuer = iss.issuerURL
	}
	if cfg.Audience == "" {
		cfg.Audience = "gridctl"
	}
	v, err := New(context.Background(), cfg, iss.srv.Client())
	if err != nil {
		var disc *DiscoveryError
		if errors.As(err, &disc) {
			t.Fatalf("New discovery: %v (cause %v)", err, disc.cause)
		}
		t.Fatalf("New: %v", err)
	}
	return v
}

func TestVerifier_AcceptAndReject(t *testing.T) {
	key := rsaKey(t, "kid-1")
	iss := newTestIssuer(t, key)
	v := newVerifierFor(t, iss, Config{Audience: "gridctl"})
	ctx := context.Background()

	good := iss.mint(t, key, tokenSpec{})
	p, err := v.Verify(ctx, good)
	if err != nil {
		t.Fatalf("valid token: %v", err)
	}
	if p.ID != "user-1" {
		t.Fatalf("principal = %q", p.ID)
	}

	within := time.Now().Add(-20 * time.Second)
	if _, err := v.Verify(ctx, iss.mint(t, key, tokenSpec{exp: within})); err != nil {
		t.Fatalf("exp within 30s: %v", err)
	}
	beyond := time.Now().Add(-31 * time.Second)
	p, err = v.Verify(ctx, iss.mint(t, key, tokenSpec{exp: beyond}))
	requireCategory(t, mustInvalid(t, p, err), CategoryExpired)

	nbfOK := time.Now().Add(4 * time.Minute)
	if _, err := v.Verify(ctx, iss.mint(t, key, tokenSpec{nbf: &nbfOK})); err != nil {
		t.Fatalf("nbf four minutes ahead: %v", err)
	}
	nbfBad := time.Now().Add(6 * time.Minute)
	p, err = v.Verify(ctx, iss.mint(t, key, tokenSpec{nbf: &nbfBad}))
	requireCategory(t, mustInvalid(t, p, err), CategoryNotYetValid)

	cases := []struct {
		name string
		spec tokenSpec
		cat  string
	}{
		{name: "wrong iss", spec: tokenSpec{iss: "https://other.example"}, cat: CategoryIssuer},
		{name: "wrong aud string", spec: tokenSpec{aud: "other"}, cat: CategoryAudience},
		{name: "wrong aud array", spec: tokenSpec{aud: []string{"other", "nope"}}, cat: CategoryAudience},
		{name: "aud array contains", spec: tokenSpec{aud: []string{"other", "gridctl"}}},
		{name: "unknown kid", spec: tokenSpec{kid: "missing"}, cat: CategoryUnknownKID},
		{name: "alg none", spec: tokenSpec{alg: jose.SignatureAlgorithm("none")}, cat: CategoryAlgorithm},
		{name: "alg hs256", spec: tokenSpec{alg: jose.HS256}, cat: CategoryAlgorithm},
		{name: "typ id-jag", spec: tokenSpec{typ: "id-jag+jwt"}, cat: CategoryTyp},
		{name: "typ uppercase access token", spec: tokenSpec{typ: "AT+JWT"}},
		{name: "typ lowercase jwt", spec: tokenSpec{typ: "jwt"}},
		{name: "typ mixed case media type", spec: tokenSpec{typ: "Application/AT+JWT"}},
		{name: "malformed", spec: tokenSpec{raw: "a.b"}, cat: CategoryMalformed},
		{name: "opaque", spec: tokenSpec{opaque: true}, cat: CategoryMalformed},
		{name: "missing sub", spec: tokenSpec{sub: 12}, cat: CategoryIdentity},
		{name: "overflow nbf", spec: tokenSpec{extra: map[string]any{"nbf": 1e100}}, cat: CategoryMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Verify(ctx, iss.mint(t, key, tc.spec))
			if tc.cat == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			requireCategory(t, mustInvalid(t, nil, err), tc.cat)
		})
	}
}

func TestVerifier_ScopesAndIdentityClaim(t *testing.T) {
	key := rsaKey(t, "kid-1")
	iss := newTestIssuer(t, key)
	v := newVerifierFor(t, iss, Config{RequiredScopes: []string{"mcp:tools", "openid"}, IdentityClaim: "email"})
	ctx := context.Background()
	_, err := v.Verify(ctx, iss.mint(t, key, tokenSpec{extra: map[string]any{"email": "a@example.com"}, scope: "openid"}))
	var scopeErr *ScopeError
	if !errors.As(err, &scopeErr) {
		t.Fatalf("missing scope: %v", err)
	}
	if _, err := v.Verify(ctx, iss.mint(t, key, tokenSpec{extra: map[string]any{"email": "a@example.com"}, scope: "openid mcp:tools"})); err != nil {
		t.Fatalf("scope string: %v", err)
	}
	if _, err := v.Verify(ctx, iss.mint(t, key, tokenSpec{extra: map[string]any{"email": "a@example.com"}, scp: []string{"mcp:tools", "openid"}})); err != nil {
		t.Fatalf("scp array: %v", err)
	}
	if _, err := v.Verify(ctx, iss.mint(t, key, tokenSpec{extra: map[string]any{"email": "a@example.com"}, scp: "openid mcp:tools"})); err != nil {
		t.Fatalf("scp string: %v", err)
	}
	_, err = v.Verify(ctx, iss.mint(t, key, tokenSpec{scope: "openid mcp:tools"}))
	requireCategory(t, mustInvalid(t, nil, err), CategoryIdentity)
}

func TestVerifier_AdvertisedAlgs(t *testing.T) {
	ec := ecKey(t, "ec-1")
	iss := newTestIssuer(t, ec)
	iss.signingAlgs = []string{"ES256"}
	v := newVerifierFor(t, iss, Config{})
	if _, err := v.Verify(context.Background(), iss.mint(t, ec, tokenSpec{})); err != nil {
		t.Fatalf("advertised ES256: %v", err)
	}

	omitted := newTestIssuer(t, ec)
	omitted.omitAlgs = true
	v2 := newVerifierFor(t, omitted, Config{Issuer: omitted.issuerURL})
	if _, err := v2.Verify(context.Background(), omitted.mint(t, ec, tokenSpec{})); err != nil {
		t.Fatalf("default list accepts ES256: %v", err)
	}

	hs := newTestIssuer(t, ec)
	hs.signingAlgs = []string{"HS256"}
	v3 := newVerifierFor(t, hs, Config{Issuer: hs.issuerURL})
	if _, err := v3.Verify(context.Background(), hs.mint(t, ec, tokenSpec{})); err != nil {
		t.Fatalf("HS256-only advertisement falls back to ES256 support: %v", err)
	}
	_, err := v3.Verify(context.Background(), hs.mint(t, ec, tokenSpec{alg: jose.HS256}))
	requireCategory(t, mustInvalid(t, nil, err), CategoryAlgorithm)
}

func TestVerifier_DiscoveryUnavailable(t *testing.T) {
	key := rsaKey(t, "kid-1")
	iss := newTestIssuer(t, key)
	iss.allNotFound.Store(true)
	v, err := New(context.Background(), Config{Issuer: iss.issuerURL, Audience: "gridctl"}, iss.srv.Client())
	if err == nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("New err = %v", err)
	}
	var disc *DiscoveryError
	if !errors.As(err, &disc) || disc.Category != CategoryDiscoveryUnavailable {
		t.Fatalf("category = %#v", err)
	}
	if readyErr := v.Ready(context.Background()); !errors.Is(readyErr, ErrUnavailable) {
		t.Fatalf("Ready = %v", readyErr)
	}
}

func TestVerifier_KidRotationAndBurst(t *testing.T) {
	first := rsaKey(t, "kid-1")
	second := rsaKey(t, "kid-2")
	iss := newTestIssuer(t, first)
	v := newVerifierFor(t, iss, Config{})
	ctx := context.Background()
	if _, err := v.Verify(ctx, iss.mint(t, first, tokenSpec{})); err != nil {
		t.Fatal(err)
	}
	afterFirst := iss.jwksFetches.Load()
	if afterFirst < 1 {
		t.Fatalf("expected a JWKS fetch, got %d", afterFirst)
	}
	iss.setKeys(first, second)
	if _, err := v.Verify(ctx, iss.mint(t, second, tokenSpec{kid: "kid-2"})); err != nil {
		t.Fatalf("rotated kid: %v", err)
	}
	afterRotate := iss.jwksFetches.Load()
	if afterRotate != afterFirst+1 {
		t.Fatalf("rotation fetches = %d, want %d", afterRotate, afterFirst+1)
	}

	iss.jwksDelay.Store(int64(150 * time.Millisecond))
	defer iss.jwksDelay.Store(0)
	before := iss.jwksFetches.Load()
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 8)
	token := iss.mint(t, second, tokenSpec{kid: "kid-unknown"})
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := v.Verify(ctx, token)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		requireCategory(t, mustInvalid(t, nil, err), CategoryUnknownKID)
	}
	if got := iss.jwksFetches.Load() - before; got != 1 {
		t.Fatalf("burst fetches = %d, want 1", got)
	}
}

func TestVerifier_RetryAfterDiscoveryFailure(t *testing.T) {
	key := rsaKey(t, "kid-1")
	iss := newTestIssuer(t, key)
	iss.allNotFound.Store(true)
	v, err := New(context.Background(), Config{Issuer: iss.issuerURL, Audience: "gridctl"}, iss.srv.Client())
	if err == nil {
		t.Fatal("expected startup discovery failure")
	}
	now := time.Now()
	v.now = func() time.Time { return now }
	if _, err := v.Verify(context.Background(), iss.mint(t, key, tokenSpec{})); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("down: %v", err)
	}
	iss.allNotFound.Store(false)
	now = now.Add(2 * time.Second)
	p, err := v.Verify(context.Background(), iss.mint(t, key, tokenSpec{}))
	if err != nil {
		t.Fatalf("recovered: %v", err)
	}
	if p.ID != "user-1" {
		t.Fatalf("principal = %q", p.ID)
	}
}

func TestVerifier_BackoffSkipsFetch(t *testing.T) {
	key := rsaKey(t, "kid-1")
	iss := newTestIssuer(t, key)
	iss.allNotFound.Store(true)
	v, _ := New(context.Background(), Config{Issuer: iss.issuerURL, Audience: "gridctl"}, iss.srv.Client())
	now := time.Now()
	v.now = func() time.Time { return now }
	if err := v.Ready(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	fetches := iss.metaFetches.Load()
	now = now.Add(time.Millisecond)
	if err := v.Ready(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if iss.metaFetches.Load() != fetches {
		t.Fatal("backoff did not suppress discovery")
	}
	now = now.Add(2 * time.Second)
	_ = v.Ready(context.Background())
	if iss.metaFetches.Load() == fetches {
		t.Fatal("backoff did not expire")
	}
}

func mustInvalid(t *testing.T, p *Principal, err error) error {
	t.Helper()
	if p != nil {
		t.Fatal("expected no principal")
	}
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("not invalid_token: %v", err)
	}
	return err
}

func requireCategory(t *testing.T, err error, category string) {
	t.Helper()
	var tok *TokenError
	if !errors.As(err, &tok) || tok.Category != category {
		t.Fatalf("category = %v, want %s (cause %v)", err, category, errors.Unwrap(err))
	}
}
