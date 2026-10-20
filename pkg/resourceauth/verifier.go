package resourceauth

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Config is the resource-server verification configuration. IdentityClaim
// empty means "sub".
type Config struct {
	Issuer         string
	Audience       string
	JWKSURL        string
	RequiredScopes []string
	IdentityClaim  string
}

// Principal is a verified access token's identity. ID is the identity claim.
// Scopes is the union of scope and scp. Expiry is the token exp.
type Principal struct {
	ID     string
	Scopes []string
	Expiry time.Time
}

// Verifier validates RFC 9068 access tokens against an external issuer.
// A failed startup discovery does not make New fail closed: Ready retries
// with backoff, and protected routes answer 503 until a key set is available.
type Verifier struct {
	cfg    Config
	client *http.Client
	keyCtx context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	verifier  *oidc.IDTokenVerifier
	keySet    oidc.KeySet
	algs      []string
	nextRetry time.Time
	backoff   time.Duration
	now       func() time.Time
	knownKids map[string]bool
}

// New builds a verifier and attempts discovery once, bounded by 10 seconds.
// The returned verifier is usable when err is non-nil: Ready retries. ctx is
// the daemon lifetime context used for later JWKS refreshes. client, when
// nil, is an http.Client with a 10-second timeout.
func New(ctx context.Context, cfg Config, client *http.Client) (*Verifier, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = &http.Client{Timeout: discoveryTimeout}
	}
	if cfg.IdentityClaim == "" {
		cfg.IdentityClaim = "sub"
	}
	ctx, cancel := context.WithCancel(ctx)
	// RemoteKeySet strips cancellation from its configuration context. Bind
	// the HTTP transport too so background refreshes stop with the daemon.
	keyClient := *client
	transport := keyClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	keyClient.Transport = lifetimeTransport{ctx: ctx, next: transport}
	v := &Verifier{
		cfg:    cfg,
		client: client,
		keyCtx: oidc.ClientContext(ctx, &keyClient),
		cancel: cancel,
		now:    time.Now,
	}
	v.mu.Lock()
	err := v.discoverLocked(ctx)
	v.mu.Unlock()
	return v, err
}

type lifetimeTransport struct {
	ctx  context.Context
	next http.RoundTripper
}

type lifetimeBody struct {
	io.ReadCloser
	cancel context.CancelFunc
	stop   func() bool
}

func (b *lifetimeBody) Close() error {
	b.stop()
	b.cancel()
	return b.ReadCloser.Close()
}

func (t lifetimeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(t.ctx, cancel)
	if t.ctx.Err() != nil {
		cancel()
	}
	resp, err := t.next.RoundTrip(r.Clone(ctx))
	if err != nil {
		stop()
		cancel()
		return nil, err
	}
	resp.Body = &lifetimeBody{ReadCloser: resp.Body, cancel: cancel, stop: stop}
	return resp, nil
}

// Close cancels outstanding JWKS fetches when the owning server shuts down.
func (v *Verifier) Close() {
	if v != nil && v.cancel != nil {
		v.cancel()
	}
}

// Ready reports whether a key set is available. A missing key set is retried
// with exponential backoff capped at 60 seconds. The first call after a
// failed New retries immediately.
func (v *Verifier) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if v == nil {
		return discoveryUnavailable(ErrUnavailable)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.verifier != nil {
		return nil
	}
	now := v.now()
	if !v.nextRetry.IsZero() && now.Before(v.nextRetry) {
		return discoveryUnavailable(ErrUnavailable)
	}
	err := v.discoverLocked(ctx)
	if err != nil {
		delay := v.backoff
		if delay <= 0 {
			delay = time.Second
		}
		v.nextRetry = now.Add(delay)
		next := delay * 2
		if next > maxBackoff {
			next = maxBackoff
		}
		v.backoff = next
		return err
	}
	v.nextRetry = time.Time{}
	v.backoff = 0
	return nil
}

// Verify checks raw as an access token. A missing key set returns
// ErrUnavailable. A rejected token returns ErrInvalidToken. A missing
// required scope returns ErrInsufficientScope.
func (v *Verifier) Verify(ctx context.Context, raw string) (*Principal, error) {
	if err := v.Ready(ctx); err != nil {
		return nil, err
	}
	if err := checkTyp(raw); err != nil {
		return nil, err
	}
	v.mu.Lock()
	verifier := v.verifier
	v.mu.Unlock()
	if verifier == nil {
		return nil, discoveryUnavailable(ErrUnavailable)
	}
	idToken, err := verifier.Verify(ctx, raw)
	if err != nil {
		if isKeyFetchFailure(err) {
			return nil, discoveryUnavailable(err)
		}
		cat := classifyVerifyError(err, v.cfg.IdentityClaim)
		if cat == CategorySignature && !v.kidKnown(headerKid(raw)) {
			// go-oidc refreshes once on an unknown kid and then reports a
			// generic signature failure. A kid we have never accepted is
			// that case.
			cat = CategoryUnknownKID
		}
		return nil, invalidTokenCause(cat, err)
	}
	// go-oidc tolerates Google's legacy scheme-less issuer for ID tokens.
	// Resource-server tokens require exact issuer equality.
	if idToken.Issuer != v.cfg.Issuer {
		return nil, invalidToken(CategoryIssuer)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v.rememberKid(headerKid(raw))
	return principalFromToken(v.cfg, idToken)
}

func (v *Verifier) kidKnown(kid string) bool {
	if kid == "" {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.knownKids[kid]
}

func (v *Verifier) rememberKid(kid string) {
	if kid == "" {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.knownKids == nil {
		v.knownKids = map[string]bool{}
	}
	v.knownKids[kid] = true
}

func oidcRemoteKeySet(ctx context.Context, jwksURL string) oidc.KeySet {
	return oidc.NewRemoteKeySet(ctx, jwksURL)
}

func newOIDCVerifier(issuer string, keySet oidc.KeySet, audience string, algs []string) *oidc.IDTokenVerifier {
	if len(algs) == 0 {
		algs = append([]string(nil), defaultSigningAlgs...)
	}
	return oidc.NewVerifier(issuer, keySet, &oidc.Config{
		ClientID:             audience,
		SupportedSigningAlgs: algs,
		SkipExpiryCheck:      true,
	})
}
