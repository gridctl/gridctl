package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"
)

const (
	// gatewayStatusTimeout bounds a /api/var/status read.
	gatewayStatusTimeout = 2 * time.Second
	// gatewaySessionTimeout bounds a /api/var/unlock or /api/var/relock call.
	gatewaySessionTimeout = 10 * time.Second
)

// Per-gateway outcomes of var unlock and var lock.
const (
	gatewayUnlocked        = "unlocked"
	gatewayAlreadyUnlocked = "already_unlocked"
	gatewayLocked          = "locked"
	gatewayAlreadyLocked   = "already_locked"
	gatewayFailed          = "failed"
)

// runningGateways lists the running daemons, lowest port first. state.List
// only reads this home's state directory, so IsRunning is the only filter
// needed: it drops state files left behind by daemons that have exited.
func runningGateways() ([]state.DaemonState, error) {
	states, err := state.List()
	if err != nil {
		return nil, err
	}
	var running []state.DaemonState
	for i := range states {
		if state.IsRunning(&states[i]) {
			running = append(running, states[i])
		}
	}
	sort.SliceStable(running, func(i, j int) bool {
		if running[i].Port != running[j].Port {
			return running[i].Port < running[j].Port
		}
		return running[i].StackName < running[j].StackName
	})
	return running, nil
}

// gatewayVarStatus is the subset of GET /api/var/status the CLI reads.
type gatewayVarStatus struct {
	Encrypted    bool   `json:"encrypted"`
	Locked       bool   `json:"locked"`
	UnlockSource string `json:"unlock_source,omitempty"`
}

// label renders the store state for status, doctor, and explain. An unlocked
// encrypted store with no recorded source is plain "unlocked".
func (v gatewayVarStatus) label() string {
	switch {
	case !v.Encrypted:
		return "plaintext"
	case v.Locked:
		return "locked"
	case v.UnlockSource == "":
		return "unlocked"
	default:
		return "unlocked (" + v.UnlockSource + ")"
	}
}

// readGatewayVarStatus reads /api/var/status from a running daemon.
func readGatewayVarStatus(ctx context.Context, st state.DaemonState) (gatewayVarStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, gatewayStatusTimeout)
	defer cancel()
	api := newDaemonAPIFor(st, gatewayStatusTimeout)
	resp, err := api.DoContext(ctx, http.MethodGet, api.URL("/api/var/status"), nil)
	if err != nil {
		return gatewayVarStatus{}, err
	}
	defer resp.Body.Close()
	if authErr := api.describeUnauthorized(resp); authErr != nil {
		return gatewayVarStatus{}, authErr
	}
	if resp.StatusCode != http.StatusOK {
		return gatewayVarStatus{}, fmt.Errorf("status %d", resp.StatusCode)
	}
	var out gatewayVarStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil {
		return gatewayVarStatus{}, err
	}
	return out, nil
}

// gatewayVarState pairs a running daemon with its store state, or the error
// that kept it from being read.
type gatewayVarState struct {
	State  state.DaemonState
	Status gatewayVarStatus
	Err    error
}

// runningGatewayVarStates reads the store state of every running daemon.
func runningGatewayVarStates(ctx context.Context) ([]gatewayVarState, error) {
	gateways, err := runningGateways()
	if err != nil {
		return nil, err
	}
	out := make([]gatewayVarState, 0, len(gateways))
	for _, st := range gateways {
		status, err := readGatewayVarStatus(ctx, st)
		out = append(out, gatewayVarState{State: st, Status: status, Err: err})
	}
	return out, nil
}

// gatewaySessionResult is one gateway's outcome in var unlock and var lock.
type gatewaySessionResult struct {
	Stack  string `json:"stack"`
	Port   int    `json:"port"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func (r gatewaySessionResult) name() string {
	return fmt.Sprintf("%s (%d)", r.Stack, r.Port)
}

// postGatewaySession posts body to path on a running daemon and returns the
// "status" field of a 200 response, or a reason the call failed. Reasons
// never carry the request body.
func postGatewaySession(ctx context.Context, st state.DaemonState, path string, body []byte) gatewaySessionResult {
	result := gatewaySessionResult{Stack: st.StackName, Port: st.Port, Status: gatewayFailed}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, gatewaySessionTimeout)
	defer cancel()
	api := newDaemonAPIFor(st, gatewaySessionTimeout)
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	resp, err := api.DoContext(ctx, http.MethodPost, api.URL(path), reader)
	if err != nil {
		if ctx.Err() != nil {
			result.Error = "timed out or cancelled"
		} else {
			result.Error = "unreachable"
		}
		return result
	}
	defer resp.Body.Close()

	var payload struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload)

	switch {
	case resp.StatusCode == http.StatusOK && payload.Status != "":
		result.Status = payload.Status
		return result
	case resp.StatusCode == http.StatusUnauthorized && api.describeUnauthorized(resp) != nil:
		result.Error = "unauthorized; set " + gatewayTokenEnv
	case resp.StatusCode == http.StatusUnauthorized && resp.Header.Get("Gridctl-Auth-Rejected") == "1":
		result.Error = "unauthorized"
	case payload.Error != "":
		result.Error = fmt.Sprintf("%d %s", resp.StatusCode, sanitizeTerminal(payload.Error))
	default:
		result.Error = fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return result
}

// varUnlockDocument is the --format json shape of gridctl var unlock.
type varUnlockDocument struct {
	SchemaVersion int                    `json:"schema_version"`
	Verified      bool                   `json:"verified"`
	Gateways      []gatewaySessionResult `json:"gateways"`
}

const noGatewayUnlockNotice = "No running gateway. Passphrase verified; gridctl apply will prompt for it when a stack needs the store."

// runVarUnlockSession verifies the passphrase against the local store and
// unlocks the store in every running gateway. It returns the exit code: 0
// when the passphrase verified and no gateway failed, 1 on a wrong
// passphrase or a failed gateway, 2 when the state directory or local store
// cannot be read. A plaintext store has nothing to unlock and exits 0.
func runVarUnlockSession(ctx context.Context, stdout, stderr io.Writer, format string) int {
	asJSON := format == "json"
	doc := varUnlockDocument{SchemaVersion: 1, Gateways: []gatewaySessionResult{}}
	emit := func() {
		if asJSON {
			_ = writeJSONOutput(stdout, doc)
		}
	}

	store, err := loadVault()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if !store.IsEncrypted() {
		if asJSON {
			emit()
		} else {
			fmt.Fprintln(stdout, "Variable store is not encrypted; nothing to unlock")
		}
		return 0
	}
	gateways, err := runningGateways()
	if err != nil {
		fmt.Fprintf(stderr, "reading gateway state: %v\n", err)
		return 2
	}

	pass, err := promptPassphrase("Vault passphrase: ")
	if err != nil {
		fmt.Fprintln(stderr, err)
		emit()
		return 1
	}
	if err := store.Unlock(pass); err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, vault.ErrWrongPassphrase) {
			emit()
			return 1
		}
		return 2
	}
	doc.Verified = true

	body, err := json.Marshal(map[string]string{"passphrase": pass, "source": vault.UnlockSourceCLI})
	if err != nil {
		fmt.Fprintln(stderr, "encoding unlock request")
		return 1
	}
	code := 0
	for _, st := range gateways {
		result := postGatewaySession(ctx, st, "/api/var/unlock", body)
		if result.Status != gatewayUnlocked && result.Status != gatewayAlreadyUnlocked && result.Status != gatewayFailed {
			result.Error = "unexpected status " + sanitizeTerminal(result.Status)
			result.Status = gatewayFailed
		}
		if result.Status == gatewayFailed {
			code = 1
		}
		doc.Gateways = append(doc.Gateways, result)
	}

	if asJSON {
		emit()
		return code
	}
	if len(gateways) == 0 {
		fmt.Fprintln(stdout, noGatewayUnlockNotice)
		return code
	}
	fmt.Fprintln(stdout, "Passphrase verified")
	for _, r := range doc.Gateways {
		switch r.Status {
		case gatewayUnlocked:
			fmt.Fprintf(stdout, "  %s: unlocked\n", r.name())
		case gatewayAlreadyUnlocked:
			fmt.Fprintf(stdout, "  %s: already unlocked\n", r.name())
		default:
			fmt.Fprintf(stdout, "  %s: failed: %s\n", r.name(), r.Error)
		}
	}
	return code
}

// runVarLockSession encrypts a plaintext store with a new passphrase, or,
// on an encrypted store, ends the unlocked session in every running gateway
// without prompting and without rewriting the file.
func runVarLockSession(ctx context.Context, stdout, stderr io.Writer) error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if !store.IsEncrypted() {
		// Running daemons lock themselves on their next read: the
		// plaintext-to-encrypted reload drops their variables.
		pass, err := promptPassphraseConfirm()
		if err != nil {
			return err
		}
		if err := store.Lock(pass); err != nil {
			return err
		}
		output.New().Info("Variable store locked", "encryption", "XChaCha20-Poly1305", "kdf", "Argon2id")
		return nil
	}

	gateways, err := runningGateways()
	if err != nil {
		return fmt.Errorf("reading gateway state: %w", err)
	}
	if len(gateways) == 0 {
		fmt.Fprintln(stdout, "Store is already encrypted; no running gateway to lock")
		return nil
	}

	var locked []string
	var failed []gatewaySessionResult
	for _, st := range gateways {
		result := postGatewaySession(ctx, st, "/api/var/relock", nil)
		switch result.Status {
		case gatewayLocked, gatewayAlreadyLocked:
			locked = append(locked, result.name())
		case gatewayFailed:
			failed = append(failed, result)
		default:
			result.Error = "unexpected status " + sanitizeTerminal(result.Status)
			failed = append(failed, result)
		}
	}

	line := fmt.Sprintf("Store is already encrypted. Locked in %d running gateway(s)", len(locked))
	if len(locked) > 0 {
		line += ": " + strings.Join(locked, ", ")
	}
	fmt.Fprintln(stdout, line)
	for _, r := range failed {
		fmt.Fprintf(stderr, "  %s: failed: %s\n", r.name(), r.Error)
	}
	if len(failed) > 0 {
		return fmt.Errorf("could not lock %d running gateway(s)", len(failed))
	}
	return nil
}

// vaultDoctorMessage describes an encrypted store and which running
// gateways hold it unlocked, for the doctor vault check.
func vaultDoctorMessage(gateways []gatewayVarState) string {
	if len(gateways) == 0 {
		return "encrypted at rest and locked; gridctl apply prompts for the passphrase on a terminal"
	}
	var unlocked, locked, unknown []string
	for _, g := range gateways {
		name := g.State.StackName
		switch {
		case g.Err != nil:
			unknown = append(unknown, name)
		case g.Status.Locked:
			locked = append(locked, name)
		case g.Status.Encrypted && g.Status.UnlockSource != "":
			unlocked = append(unlocked, name+" ("+g.Status.UnlockSource+")")
		default:
			unlocked = append(unlocked, name)
		}
	}
	parts := []string{"encrypted at rest"}
	if len(unlocked) > 0 {
		parts = append(parts, "unlocked in "+strings.Join(unlocked, ", "))
	}
	if len(locked) > 0 {
		parts = append(parts, "locked in "+strings.Join(locked, ", "))
	}
	if len(unknown) > 0 {
		parts = append(parts, "state unavailable in "+strings.Join(unknown, ", "))
	}
	parts = append(parts, "gridctl var unlock unlocks running gateways, gridctl apply prompts on a terminal")
	return strings.Join(parts, "; ")
}

// unlockedGatewayNames lists running gateways that report the store
// encrypted and unlocked, lowest port first.
func unlockedGatewayNames(gateways []gatewayVarState) []string {
	var names []string
	for _, g := range gateways {
		if g.Err == nil && g.Status.Encrypted && !g.Status.Locked {
			names = append(names, g.State.StackName)
		}
	}
	return names
}

// lockedExplainProblem is the var explain problem text for a store that is
// locked in this process.
func lockedExplainProblem(unlockedIn []string) string {
	switch len(unlockedIn) {
	case 0:
		return "store is locked; stored metadata and resolution are unknown"
	case 1:
		return "store is locked in this process; gateway " + unlockedIn[0] + " holds it unlocked, so apply and var commands use the gateway"
	default:
		// Var commands use the lowest port, which runningGateways lists first.
		return "store is locked in this process; gateways " + strings.Join(unlockedIn, ", ") + " hold it unlocked, so apply and var commands use gateway " + unlockedIn[0]
	}
}
