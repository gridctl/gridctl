package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"
)

// gatewayVarOpTimeout bounds one variable request to a running gateway.
const gatewayVarOpTimeout = 10 * time.Second

// gatewayVarBodyLimit caps a variable response read from a gateway.
const gatewayVarBodyLimit = 16 << 20

// errVarStoreLocked reports that no passphrase source or unlocked gateway is
// available and the terminal prompt was not used.
var errVarStoreLocked = errors.New("vault is locked. Set GRIDCTL_VAULT_PASSPHRASE or GRIDCTL_VAULT_PASSPHRASE_FILE, start the stack with gridctl apply, or run this command in a terminal to be prompted")

// varBackend is the store the var subcommands read and write: the local
// file, or a running gateway that holds it unlocked.
type varBackend interface {
	// List returns every variable sorted by key. A gateway backend returns
	// metadata only; use GetVariable or listVariableValues for values.
	List() ([]vault.Variable, error)
	GetVariable(key string) (vault.Variable, bool, error)
	Has(key string) (bool, error)
	SetVariable(v vault.Variable) error
	Delete(key string) error
	ImportVariables(vars []vault.Variable) (vault.ImportResult, error)
	ListSets() ([]vault.SetSummary, error)
	CreateSet(name string) error
	DeleteSet(name string) error
}

// localVarBackend is the store file opened and unlocked in this process.
type localVarBackend struct{ store *vault.Store }

func (l localVarBackend) List() ([]vault.Variable, error) { return l.store.List(), nil }

func (l localVarBackend) GetVariable(key string) (vault.Variable, bool, error) {
	v, ok := l.store.GetVariable(key)
	return v, ok, nil
}

func (l localVarBackend) Has(key string) (bool, error) { return l.store.Has(key), nil }

func (l localVarBackend) SetVariable(v vault.Variable) error { return l.store.SetVariable(v) }

func (l localVarBackend) Delete(key string) error { return l.store.Delete(key) }

func (l localVarBackend) ImportVariables(vars []vault.Variable) (vault.ImportResult, error) {
	return l.store.ImportVariables(vars)
}

func (l localVarBackend) ListSets() ([]vault.SetSummary, error) { return l.store.ListSets(), nil }

func (l localVarBackend) CreateSet(name string) error { return l.store.CreateSet(name) }

func (l localVarBackend) DeleteSet(name string) error { return l.store.DeleteSet(name) }

// gatewayVarBackend reads and writes the store through a running gateway's
// /api/var routes, using the credentials recorded in its state file.
type gatewayVarBackend struct {
	ctx context.Context
	st  state.DaemonState
	api *daemonAPI
}

func newGatewayVarBackend(ctx context.Context, st state.DaemonState) *gatewayVarBackend {
	if ctx == nil {
		ctx = context.Background()
	}
	return &gatewayVarBackend{ctx: ctx, st: st, api: newDaemonAPIFor(st, gatewayVarOpTimeout)}
}

// gatewayVarError is a failed variable request to a gateway. It never
// carries a request body or a value.
type gatewayVarError struct {
	gateway string
	reason  string
}

func (e *gatewayVarError) Error() string { return "gateway " + e.gateway + ": " + e.reason }

// call issues one request. A 404 returns found=false with no error when
// allowNotFound is set; every other non-2xx status is a gatewayVarError.
func (g *gatewayVarBackend) call(method, path string, in, out any, allowNotFound bool) (found bool, err error) {
	ctx, cancel := context.WithTimeout(g.ctx, gatewayVarOpTimeout)
	defer cancel()
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return false, &gatewayVarError{gateway: g.st.StackName, reason: "encoding request"}
		}
		body = bytes.NewReader(data)
	}
	resp, err := g.api.DoContext(ctx, method, g.api.URL(path), body)
	if err != nil {
		reason := "unreachable"
		if ctx.Err() != nil {
			reason = "timed out or cancelled"
		}
		return false, &gatewayVarError{gateway: g.st.StackName, reason: reason}
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, gatewayVarBodyLimit)

	if resp.StatusCode == http.StatusNotFound && allowNotFound {
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return false, &gatewayVarError{gateway: g.st.StackName, reason: g.describeFailure(resp, limited)}
	}
	if out != nil {
		if err := json.NewDecoder(limited).Decode(out); err != nil {
			return false, &gatewayVarError{gateway: g.st.StackName, reason: "unreadable response"}
		}
	}
	return true, nil
}

// describeFailure renders "<status> <message>" from a failed response.
func (g *gatewayVarBackend) describeFailure(resp *http.Response, body io.Reader) string {
	if authErr := g.api.describeUnauthorized(resp); authErr != nil {
		return fmt.Sprintf("%d %s", resp.StatusCode, authErr.Error())
	}
	var payload struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(body).Decode(&payload)
	if payload.Error != "" {
		return fmt.Sprintf("%d %s", resp.StatusCode, sanitizeTerminal(payload.Error))
	}
	return fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
}

func (g *gatewayVarBackend) List() ([]vault.Variable, error) {
	var out []vault.Variable
	if _, err := g.call(http.MethodGet, "/api/var", nil, &out, false); err != nil {
		return nil, err
	}
	if out == nil {
		out = []vault.Variable{}
	}
	return out, nil
}

func (g *gatewayVarBackend) GetVariable(key string) (vault.Variable, bool, error) {
	var v vault.Variable
	found, err := g.call(http.MethodGet, "/api/var/"+url.PathEscape(key), nil, &v, true)
	if err != nil || !found {
		return vault.Variable{}, false, err
	}
	return v, true, nil
}

// Has checks the value-free list so no value crosses the loopback.
func (g *gatewayVarBackend) Has(key string) (bool, error) {
	vars, err := g.List()
	if err != nil {
		return false, err
	}
	for _, v := range vars {
		if v.Key == key {
			return true, nil
		}
	}
	return false, nil
}

// SetVariable sends every field so the gateway's partial-update PUT
// replaces the record the way the local SetVariable does.
func (g *gatewayVarBackend) SetVariable(v vault.Variable) error {
	if v.Type == "" {
		v.Type = vault.TypeString
	}
	body := map[string]any{
		"value":       v.Value,
		"type":        v.Type,
		"is_secret":   v.IsSecret,
		"set":         v.Set,
		"description": v.Description,
		"docs":        v.Docs,
		"example":     v.Example,
		"deprecated":  v.Deprecated,
	}
	_, err := g.call(http.MethodPut, "/api/var/"+url.PathEscape(v.Key), body, nil, false)
	return err
}

func (g *gatewayVarBackend) Delete(key string) error {
	_, err := g.call(http.MethodDelete, "/api/var/"+url.PathEscape(key), nil, nil, false)
	return err
}

func (g *gatewayVarBackend) ImportVariables(vars []vault.Variable) (vault.ImportResult, error) {
	result := vault.ImportResult{Skipped: []string{}}
	if len(vars) == 0 {
		// The route rejects an empty import; the local store imports nothing.
		return result, nil
	}
	if _, err := g.call(http.MethodPost, "/api/var/import", map[string]any{"variables": vars}, &result, false); err != nil {
		return vault.ImportResult{}, err
	}
	if result.Skipped == nil {
		result.Skipped = []string{}
	}
	return result, nil
}

func (g *gatewayVarBackend) ListSets() ([]vault.SetSummary, error) {
	var out []vault.SetSummary
	if _, err := g.call(http.MethodGet, "/api/var/sets", nil, &out, false); err != nil {
		return nil, err
	}
	if out == nil {
		out = []vault.SetSummary{}
	}
	return out, nil
}

func (g *gatewayVarBackend) CreateSet(name string) error {
	_, err := g.call(http.MethodPost, "/api/var/sets", map[string]string{"name": name}, nil, false)
	return err
}

func (g *gatewayVarBackend) DeleteSet(name string) error {
	_, err := g.call(http.MethodDelete, "/api/var/sets/"+url.PathEscape(name), nil, nil, false)
	return err
}

// listVariableValues returns every variable with its value, sorted by key.
// A gateway backend fetches each key; a key deleted between the list and
// its fetch is left out.
func listVariableValues(b varBackend) ([]vault.Variable, error) {
	vars, err := b.List()
	if err != nil {
		return nil, err
	}
	if _, local := b.(localVarBackend); local {
		return vars, nil
	}
	return fetchVariableValues(b, vars)
}

// fetchVariableValues fetches the full record of each listed variable from
// a gateway backend. A local backend's records already carry values.
func fetchVariableValues(b varBackend, vars []vault.Variable) ([]vault.Variable, error) {
	if _, local := b.(localVarBackend); local {
		return vars, nil
	}
	out := make([]vault.Variable, 0, len(vars))
	for _, meta := range vars {
		v, ok, err := b.GetVariable(meta.Key)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, v)
		}
	}
	return out, nil
}

// openVarBackend resolves the store for a var subcommand: the local store
// if plaintext or already unlocked, GRIDCTL_VAULT_PASSPHRASE,
// GRIDCTL_VAULT_PASSPHRASE_FILE, a running gateway that holds it unlocked
// (lowest port first), a terminal prompt, and otherwise errVarStoreLocked.
// With prompt false the terminal step is skipped.
func openVarBackend(ctx context.Context, prompt bool) (varBackend, error) {
	store, err := loadVault()
	if err != nil {
		return nil, err
	}
	if !store.IsLocked() {
		return localVarBackend{store: store}, nil
	}
	if handled, err := unlockStoreFromEnvironment(store); err != nil {
		return nil, err
	} else if handled {
		return localVarBackend{store: store}, nil
	}

	gateway, err := selectVarGateway(ctx)
	if err != nil {
		return nil, err
	}
	if gateway != nil {
		slog.Debug("using running gateway for the variable store", "stack", gateway.st.StackName, "port", gateway.st.Port)
		return gateway, nil
	}

	if !prompt || !stdinIsTerminal() {
		return nil, errVarStoreLocked
	}
	pass, err := readHiddenPassphrase("Vault passphrase: ")
	if err != nil {
		return nil, err
	}
	if err := store.Unlock(pass); err != nil {
		return nil, err
	}
	return localVarBackend{store: store}, nil
}

// selectVarGateway returns the lowest-port running gateway that reports the
// store encrypted and unlocked, or nil when none does. A gateway that is
// unreachable, answers 401 or 423, or reports the store locked is skipped;
// any other failed status read is returned with the gateway's name.
func selectVarGateway(ctx context.Context) (*gatewayVarBackend, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	gateways, err := runningGateways()
	if err != nil {
		slog.Debug("reading gateway state", "error", err)
		return nil, nil
	}
	for _, st := range gateways {
		ok, err := probeVarGateway(ctx, st)
		if err != nil {
			return nil, err
		}
		if ok {
			return newGatewayVarBackend(ctx, st), nil
		}
	}
	return nil, nil
}

// probeVarGateway reads /api/var/status and reports whether the gateway can
// serve variable commands.
func probeVarGateway(ctx context.Context, st state.DaemonState) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayStatusTimeout)
	defer cancel()
	api := newDaemonAPIFor(st, gatewayStatusTimeout)
	resp, err := api.DoContext(ctx, http.MethodGet, api.URL("/api/var/status"), nil)
	if err != nil {
		slog.Debug("skipping unreachable gateway", "stack", st.StackName, "port", st.Port)
		return false, nil
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 64<<10)
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusLocked:
		slog.Debug("skipping gateway", "stack", st.StackName, "port", st.Port, "status", resp.StatusCode)
		return false, nil
	default:
		g := &gatewayVarBackend{st: st, api: api}
		return false, &gatewayVarError{gateway: st.StackName, reason: g.describeFailure(resp, limited)}
	}
	var status gatewayVarStatus
	if err := json.NewDecoder(limited).Decode(&status); err != nil {
		return false, &gatewayVarError{gateway: st.StackName, reason: "unreadable response"}
	}
	return status.Encrypted && !status.Locked, nil
}

// unlockStoreFromEnvironment unlocks store from GRIDCTL_VAULT_PASSPHRASE,
// else GRIDCTL_VAULT_PASSPHRASE_FILE. handled is false when neither is set.
// A rejected value is an error, never a fallback.
func unlockStoreFromEnvironment(store *vault.Store) (handled bool, err error) {
	if pass := os.Getenv("GRIDCTL_VAULT_PASSPHRASE"); pass != "" {
		return true, store.Unlock(pass)
	}
	filePass, ok, err := passphraseFromFile()
	if err != nil {
		return true, err
	}
	if !ok {
		return false, nil
	}
	if err := store.Unlock(filePass); err != nil {
		return true, fmt.Errorf("unlocking vault from %s: passphrase rejected", vault.PassphraseFileEnv)
	}
	return true, nil
}
