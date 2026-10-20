package main

import (
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"strconv"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
)

// gatewayTokenEnv is the access token sent to an OAuth-mode gateway. It is
// also accepted in bearer and api_key mode and wins over the recorded state token.
const gatewayTokenEnv = "GRIDCTL_GATEWAY_TOKEN" // #nosec G101 -- Environment variable name, not a credential.

// daemonAPI issues authenticated requests against a running gateway's local
// API. Every subcommand that talks to /api/* goes through here, because
// gateway.auth is opt-in and the credentials must be attached uniformly: a
// per-command approach leaves whichever command was missed returning 401 the
// moment a user enables auth.
//
// Credentials come from the daemon state file, which already carries the
// port these calls need. The daemon records the token there already
// resolved, so the CLI never loads the stack or unlocks the vault.
type daemonAPI struct {
	port       int
	token      string
	header     string
	scheme     string // "bearer", "api_key", or "oauth"
	issuerHost string
	envBearer  bool
	client     *http.Client
}

// newDaemonAPI builds a client for the daemon on the given port, adopting
// whatever auth that daemon recorded. States are matched by port so a
// subcommand already holding one does not have to re-read it.
func newDaemonAPI(port int, timeout time.Duration) *daemonAPI {
	api := &daemonAPI{port: port, client: &http.Client{Timeout: timeout}}
	if st, ok := daemonStateForPort(port); ok {
		api.adoptState(st)
	}
	api.applyEnvToken()
	return api
}

// newDaemonAPIFor builds a client from an already-loaded state, avoiding a
// second read for callers that have one.
func newDaemonAPIFor(st state.DaemonState, timeout time.Duration) *daemonAPI {
	api := &daemonAPI{port: st.Port, client: &http.Client{Timeout: timeout}}
	api.adoptState(st)
	api.applyEnvToken()
	return api
}

func (a *daemonAPI) adoptState(st state.DaemonState) {
	a.token, a.header, a.scheme = st.AuthToken, st.AuthHeader, st.AuthType
	a.issuerHost = st.AuthIssuerHost
}

func (a *daemonAPI) applyEnvToken() {
	if tok := os.Getenv(gatewayTokenEnv); tok != "" {
		a.token = tok
		a.header = "Authorization"
		a.envBearer = true
	}
}

// daemonStateForPort finds the running daemon listening on port. Returns
// false when no state matches, in which case requests go out unauthenticated
// and the caller sees whatever the server decides — the same behavior as
// before auth was recorded at all.
func daemonStateForPort(port int) (state.DaemonState, bool) {
	states, err := state.List()
	if err != nil {
		return state.DaemonState{}, false
	}
	for _, st := range states {
		if st.Port == port {
			return st, true
		}
	}
	return state.DaemonState{}, false
}

// URL builds an absolute URL for a path on the daemon.
func (a *daemonAPI) URL(path string) string {
	return fmt.Sprintf("http://localhost:%d%s", a.port, path)
}

// Do issues a request with credentials attached.
func (a *daemonAPI) Do(method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	a.authorize(req)
	return a.client.Do(req)
}

// Get issues an authenticated GET.
func (a *daemonAPI) Get(url string) (*http.Response, error) {
	return a.Do(http.MethodGet, url, nil)
}

// authorize attaches the recorded credentials. Mirrors the server's
// authMiddleware: a bearer type gets the "Bearer " prefix, anything else is
// sent as a raw header value, and the header name defaults to Authorization.
func (a *daemonAPI) authorize(req *http.Request) {
	if a.token == "" {
		return
	}
	if a.envBearer || a.scheme == "bearer" || a.scheme == "oauth" || a.scheme == "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
		return
	}
	header := a.header
	if header == "" {
		header = "Authorization"
	}
	req.Header.Set(header, a.token)
}

// describeUnauthorized reports a marked 401. In OAuth mode the text tells the
// operator to set GRIDCTL_GATEWAY_TOKEN. Other modes return nil so callers
// keep their existing wording.
func (a *daemonAPI) describeUnauthorized(resp *http.Response) error {
	if a == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		return nil
	}
	if resp.Header.Get("Gridctl-Auth-Rejected") != "1" {
		return nil
	}
	if a.scheme != "oauth" {
		return nil
	}
	host := a.issuerHost
	if host == "" {
		host = "the authorization server"
	}
	return &oauthCredentialError{host: host}
}

type oauthCredentialError struct{ host string }

func (e *oauthCredentialError) Error() string {
	return fmt.Sprintf("set GRIDCTL_GATEWAY_TOKEN to a token issued by %s", e.host)
}

// newDaemonAPIForBaseURL builds a client for a daemon addressed by base URL
// (e.g. "http://localhost:8180"), matching the recorded state by port so
// callers that only carry a URL still authenticate.
func newDaemonAPIForBaseURL(baseURL string, timeout time.Duration) *daemonAPI {
	port := 0
	if u, err := neturl.Parse(baseURL); err == nil {
		if p, cerr := strconv.Atoi(u.Port()); cerr == nil {
			port = p
		}
	}
	return newDaemonAPI(port, timeout)
}
