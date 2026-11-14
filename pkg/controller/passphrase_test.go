package controller

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"
	"golang.org/x/term"
)

const testPass = "correct-horse" // #nosec G101 -- test fixture

const needsStoreStack = `version: "1"
name: prompt-demo
network:
  name: prompt-net
mcp-servers:
  - name: remote
    url: https://mcp.example.com/mcp
    headers:
      Authorization: "Bearer ${var:DEMO_TOKEN}"
`

const noStoreStack = `version: "1"
name: prompt-plain
network:
  name: prompt-net
mcp-servers:
  - name: remote
    url: https://mcp.example.com/mcp
`

func writeStack(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeTerminal installs terminal seams. answers are returned by successive
// reads; a nil entry blocks until the test ends. It records restores.
type fakeTerminal struct {
	reads    atomic.Int32
	restores atomic.Int32
	out      bytes.Buffer
}

func installTerminal(t *testing.T, isTTY bool, answers ...[]byte) *fakeTerminal {
	t.Helper()
	ft := &fakeTerminal{}
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	prevIsTerm, prevGet, prevRestore, prevRead, prevOut := stdinIsTerminal, termGetState, termRestore, termReadPassword, promptOut
	t.Cleanup(func() {
		stdinIsTerminal, termGetState, termRestore, termReadPassword, promptOut = prevIsTerm, prevGet, prevRestore, prevRead, prevOut
	})
	stdinIsTerminal = func() bool { return isTTY }
	termGetState = func(int) (*term.State, error) { return &term.State{}, nil }
	termRestore = func(int, *term.State) error {
		ft.restores.Add(1)
		return nil
	}
	termReadPassword = func(int) ([]byte, error) {
		n := int(ft.reads.Add(1))
		if n > len(answers) {
			return nil, io.EOF
		}
		a := answers[n-1]
		if a == nil {
			<-block
			return nil, io.EOF
		}
		return a, nil
	}
	promptOut = &ft.out
	return ft
}

func setStdin(t *testing.T, r io.Reader) {
	t.Helper()
	prev := stdinSource
	stdinSource = r
	t.Cleanup(func() { stdinSource = prev })
}

func TestReadPassphraseStdin(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "newline", input: "secret\n", want: "secret"},
		{name: "crlf", input: "secret\r\n", want: "secret"},
		{name: "no newline", input: "secret", want: "secret"},
		{name: "first line only", input: "secret\nignored\n", want: "secret"},
		{name: "at cap", input: strings.Repeat("a", passphraseStdinLimit) + "\n", want: strings.Repeat("a", passphraseStdinLimit)},
		{name: "empty pipe", input: "", wantErr: "no passphrase on stdin"},
		{name: "empty line", input: "\n", wantErr: "no passphrase on stdin"},
		{name: "oversized", input: strings.Repeat("a", passphraseStdinLimit+1) + "\n", wantErr: "exceeds 4096 bytes"},
		{name: "oversized no newline", input: strings.Repeat("a", 3*passphraseStdinLimit), wantErr: "exceeds 4096 bytes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readPassphraseStdin(t.Context(), strings.NewReader(tc.input))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				if strings.Contains(err.Error(), "aaaa") {
					t.Fatalf("error echoes input: %v", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestReadPassphraseStdin_TimeoutWithWriterHeldOpen(t *testing.T) {
	prev := stdinReadTimeout
	stdinReadTimeout = 200 * time.Millisecond
	t.Cleanup(func() { stdinReadTimeout = prev })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	// The write end stays open and nothing is written, so the read blocks.
	t.Cleanup(func() { w.Close(); r.Close() })

	start := time.Now()
	_, err = readPassphraseStdin(t.Context(), r)
	if err == nil || !strings.Contains(err.Error(), "no passphrase arrived on stdin within 200ms") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("timeout took %v", time.Since(start))
	}
}

func TestReadPassphraseStdin_ContextCancel(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close(); r.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err = readPassphraseStdin(ctx, r)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "stdin") {
		t.Fatalf("err = %v, want a stdin context.Canceled error", err)
	}
}

func TestReadPassphraseStdin_Pipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	go func() {
		_, _ = io.WriteString(w, testPass+"\n")
		w.Close()
	}()
	got, err := readPassphraseStdin(t.Context(), r)
	if err != nil || got != testPass {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestAcquirePassphrase_StdinUnlocks(t *testing.T) {
	dir := lockedStoreHome(t, "DEMO_TOKEN", testPass)
	store := loadLockedStore(t, dir)
	installTerminal(t, false)
	setStdin(t, strings.NewReader(testPass+"\n"))

	sc := New(Config{PassphraseStdin: true})
	pass, err := sc.acquirePassphrase(t.Context(), store, true, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if pass != testPass {
		t.Fatal("acquirePassphrase did not return the stdin passphrase")
	}
	if store.IsLocked() {
		t.Fatal("store still locked")
	}
	if got := store.UnlockSource(); got != vault.UnlockSourceStdin {
		t.Fatalf("UnlockSource = %q, want stdin", got)
	}
	if got := logging.RedactString("x " + testPass); strings.Contains(got, testPass) {
		t.Fatalf("passphrase not registered for redaction: %q", got)
	}
}

func TestAcquirePassphrase_StdinRejected(t *testing.T) {
	dir := lockedStoreHome(t, "DEMO_TOKEN", testPass)
	store := loadLockedStore(t, dir)
	installTerminal(t, false)
	setStdin(t, strings.NewReader("wrong-value\n"))

	_, err := New(Config{PassphraseStdin: true}).acquirePassphrase(t.Context(), store, true, nil)
	if err == nil || !strings.Contains(err.Error(), "passphrase from stdin rejected") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "wrong-value") {
		t.Fatalf("error echoes the passphrase: %v", err)
	}
	if !store.IsLocked() {
		t.Fatal("store unlocked by a wrong passphrase")
	}
}

func TestAcquirePassphrase_StdinTerminalRefused(t *testing.T) {
	dir := lockedStoreHome(t, "DEMO_TOKEN", testPass)
	store := loadLockedStore(t, dir)
	ft := installTerminal(t, true)
	setStdin(t, strings.NewReader(testPass+"\n"))

	_, err := New(Config{PassphraseStdin: true}).acquirePassphrase(t.Context(), store, true, func() bool { return true })
	if !errors.Is(err, ErrPassphraseStdinTerminal) {
		t.Fatalf("err = %v, want ErrPassphraseStdinTerminal", err)
	}
	if ft.reads.Load() != 0 {
		t.Fatal("prompted despite --passphrase-stdin")
	}
	if !store.IsLocked() {
		t.Fatal("store unlocked")
	}
}

func TestAcquirePassphrase_EnvironmentTakesPrecedence(t *testing.T) {
	dir := lockedStoreHome(t, "DEMO_TOKEN", testPass)
	store := loadLockedStore(t, dir)
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", testPass)
	if err := unlockFromEnvironment(store); err != nil {
		t.Fatal(err)
	}
	if got := store.UnlockSource(); got != vault.UnlockSourceEnvironment {
		t.Fatalf("UnlockSource = %q, want environment", got)
	}

	// The stdin line is read and discarded; it would be rejected if used.
	installTerminal(t, false)
	reader := strings.NewReader("ignored-line\n")
	setStdin(t, reader)
	pass, err := New(Config{PassphraseStdin: true}).acquirePassphrase(t.Context(), store, true, nil)
	if err != nil || pass != "" {
		t.Fatalf("acquirePassphrase = %q, %v; want empty, nil", pass, err)
	}
	if reader.Len() != 0 {
		t.Fatal("stdin line was not consumed")
	}
	if got := store.UnlockSource(); got != vault.UnlockSourceEnvironment {
		t.Fatalf("UnlockSource = %q, want environment", got)
	}

	// Without the flag, an environment-unlocked store never prompts.
	ft := installTerminal(t, true, []byte(testPass))
	pass, err = New(Config{}).acquirePassphrase(t.Context(), store, true, func() bool { return true })
	if err != nil || pass != "" || ft.reads.Load() != 0 {
		t.Fatalf("prompted with the environment set: %q, %v, reads=%d", pass, err, ft.reads.Load())
	}
}

func TestUnlockFromEnvironment_RecordsPassphraseFileSource(t *testing.T) {
	dir := lockedStoreHome(t, "DEMO_TOKEN", testPass)
	store := loadLockedStore(t, dir)
	path := filepath.Join(t.TempDir(), "pass")
	if err := os.WriteFile(path, []byte(testPass+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(vault.PassphraseFileEnv, path)
	if err := unlockFromEnvironment(store); err != nil {
		t.Fatal(err)
	}
	if got := store.UnlockSource(); got != vault.UnlockSourcePassphraseFile {
		t.Fatalf("UnlockSource = %q, want passphrase_file", got)
	}
}

func TestAcquirePassphrase_PromptConditions(t *testing.T) {
	needs := writeStack(t, needsStoreStack)
	plain := writeStack(t, noStoreStack)
	broken := writeStack(t, "version: [\n")

	tests := []struct {
		name  string
		cfg   Config
		tty   bool
		allow bool
		want  bool
	}{
		{name: "prompts", cfg: Config{StackPath: needs}, tty: true, allow: true, want: true},
		{name: "stack does not need the store", cfg: Config{StackPath: plain}, tty: true, allow: true},
		{name: "pre-load fails", cfg: Config{StackPath: broken}, tty: true, allow: true},
		{name: "not a terminal", cfg: Config{StackPath: needs}, allow: true},
		{name: "daemon child", cfg: Config{StackPath: needs, DaemonChild: true}, tty: true, allow: true},
		{name: "serve never prompts", cfg: Config{StackPath: needs}, tty: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := lockedStoreHome(t, "DEMO_TOKEN", testPass)
			store := loadLockedStore(t, dir)
			ft := installTerminal(t, tc.tty, []byte(testPass))
			sc := New(tc.cfg)
			pass, err := sc.acquirePassphrase(t.Context(), store, tc.allow, func() bool { return stackNeedsStore(tc.cfg) })
			if err != nil {
				t.Fatal(err)
			}
			prompted := ft.reads.Load() > 0
			if prompted != tc.want {
				t.Fatalf("prompted = %v, want %v", prompted, tc.want)
			}
			if tc.want {
				if pass != testPass || store.IsLocked() {
					t.Fatal("prompt did not unlock the store")
				}
				if got := store.UnlockSource(); got != vault.UnlockSourceStdin {
					t.Fatalf("UnlockSource = %q, want stdin", got)
				}
				if ft.out.String() != PassphrasePrompt+"\n" {
					t.Fatalf("prompt output = %q", ft.out.String())
				}
			} else if pass != "" || !store.IsLocked() {
				t.Fatal("store unlocked without a prompt")
			}
		})
	}
}

func TestPromptPassphrase_RetriesThenFails(t *testing.T) {
	dir := lockedStoreHome(t, "DEMO_TOKEN", testPass)
	store := loadLockedStore(t, dir)
	ft := installTerminal(t, true, []byte("guess-a"), []byte("guess-b"), []byte("guess-c"), []byte(testPass))

	_, err := PromptPassphrase(t.Context(), store, os.Stdin, &ft.out)
	if err == nil || !strings.Contains(err.Error(), "three attempts") {
		t.Fatalf("err = %v", err)
	}
	if ft.reads.Load() != 3 {
		t.Fatalf("reads = %d, want 3", ft.reads.Load())
	}
	if c := strings.Count(ft.out.String(), "wrong passphrase"); c != 3 {
		t.Fatalf("wrong passphrase printed %d times: %q", c, ft.out.String())
	}
	for _, v := range []string{"guess-a", "guess-b", "guess-c"} {
		if strings.Contains(err.Error(), v) || strings.Contains(ft.out.String(), v) {
			t.Fatalf("attempt %q echoed", v)
		}
	}
	if !store.IsLocked() {
		t.Fatal("store unlocked")
	}
}

func TestPromptPassphrase_SecondAttemptSucceeds(t *testing.T) {
	dir := lockedStoreHome(t, "DEMO_TOKEN", testPass)
	store := loadLockedStore(t, dir)
	ft := installTerminal(t, true, []byte("nope"), []byte(testPass))

	pass, err := PromptPassphrase(t.Context(), store, os.Stdin, &ft.out)
	if err != nil || pass != testPass {
		t.Fatalf("PromptPassphrase = %v", err)
	}
	want := PassphrasePrompt + "\nwrong passphrase\n" + PassphrasePrompt + "\n"
	if ft.out.String() != want {
		t.Fatalf("output = %q, want %q", ft.out.String(), want)
	}
}

func TestPromptPassphrase_EOFCancels(t *testing.T) {
	dir := lockedStoreHome(t, "DEMO_TOKEN", testPass)
	store := loadLockedStore(t, dir)
	ft := installTerminal(t, true)

	_, err := PromptPassphrase(t.Context(), store, os.Stdin, &ft.out)
	if !errors.Is(err, ErrPassphraseCancelled) || err.Error() != "passphrase entry cancelled" {
		t.Fatalf("err = %v", err)
	}
}

func TestPromptPassphrase_CancelRestoresTerminal(t *testing.T) {
	dir := lockedStoreHome(t, "DEMO_TOKEN", testPass)
	store := loadLockedStore(t, dir)
	ft := installTerminal(t, true, nil)

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		for ft.reads.Load() == 0 {
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
	}()
	done := make(chan error, 1)
	go func() {
		_, err := PromptPassphrase(ctx, store, os.Stdin, &ft.out)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrPassphraseCancelled) {
			t.Fatalf("err = %v, want ErrPassphraseCancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("prompt did not return on cancel")
	}
	if ft.restores.Load() != 1 {
		t.Fatalf("terminal restored %d times, want 1", ft.restores.Load())
	}
	if ft.out.String() != PassphrasePrompt+"\n" {
		t.Fatalf("output = %q, want the prompt and a newline", ft.out.String())
	}
}

func TestDeploy_StdinPassphraseUnlocksAndIsHeldForFork(t *testing.T) {
	lockedStoreHome(t, "DEMO_TOKEN", testPass)
	installTerminal(t, false)
	setStdin(t, strings.NewReader(testPass+"\n"))
	stackPath := writeStack(t, needsStoreStack)

	// A running state for the stack stops Deploy after the load, before
	// any runtime work.
	if err := state.Save(&state.DaemonState{StackName: "prompt-demo", PID: os.Getpid(), Port: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Delete("prompt-demo") })

	sc := New(Config{StackPath: stackPath, PassphraseStdin: true, Quiet: true})
	err := sc.Deploy(t.Context())
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("err = %v, want already running", err)
	}
	if sc.vaultStore == nil || sc.vaultStore.IsLocked() {
		t.Fatal("store not unlocked from stdin")
	}
	if got := sc.vaultStore.UnlockSource(); got != vault.UnlockSourceStdin {
		t.Fatalf("UnlockSource = %q", got)
	}
	if sc.config.Passphrase != testPass {
		t.Fatal("daemon parent did not keep the passphrase for Fork")
	}
	if strings.Contains(err.Error(), testPass) {
		t.Fatal("error contains the passphrase")
	}
}

func TestDeploy_DaemonChildDoesNotHoldPassphrase(t *testing.T) {
	lockedStoreHome(t, "DEMO_TOKEN", testPass)
	installTerminal(t, false)
	setStdin(t, strings.NewReader(testPass+"\n"))
	stackPath := writeStack(t, needsStoreStack)
	if err := state.Save(&state.DaemonState{StackName: "prompt-demo", PID: os.Getpid(), Port: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Delete("prompt-demo") })

	sc := New(Config{StackPath: stackPath, PassphraseStdin: true, DaemonChild: true, Quiet: true})
	_ = sc.Deploy(t.Context())
	if sc.vaultStore == nil || sc.vaultStore.UnlockSource() != vault.UnlockSourceStdin {
		t.Fatal("daemon child did not unlock from stdin")
	}
	if sc.config.Passphrase != "" {
		t.Fatal("daemon child kept a passphrase to forward")
	}
}

func TestDeploy_PromptFailureStopsDeploy(t *testing.T) {
	lockedStoreHome(t, "DEMO_TOKEN", testPass)
	installTerminal(t, true, []byte("a"), []byte("b"), []byte("c"))
	stackPath := writeStack(t, needsStoreStack)

	err := New(Config{StackPath: stackPath, Quiet: true}).Deploy(t.Context())
	if err == nil || !strings.Contains(err.Error(), "three attempts") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeploy_WrongEnvironmentPassphraseDoesNotPrompt(t *testing.T) {
	lockedStoreHome(t, "DEMO_TOKEN", testPass)
	ft := installTerminal(t, true, []byte(testPass))
	stackPath := writeStack(t, needsStoreStack)
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "not-the-passphrase")

	err := New(Config{StackPath: stackPath, Quiet: true}).Deploy(t.Context())
	if err == nil || !strings.Contains(err.Error(), "unlocking vault") {
		t.Fatalf("err = %v, want an unlock failure", err)
	}
	if ft.reads.Load() != 0 || ft.out.Len() != 0 {
		t.Fatalf("prompted after a wrong environment passphrase: %q", ft.out.String())
	}
}

func TestDeploy_StdinErrorsStopBeforeLoad(t *testing.T) {
	lockedStoreHome(t, "DEMO_TOKEN", testPass)
	stackPath := writeStack(t, needsStoreStack)

	t.Run("terminal", func(t *testing.T) {
		installTerminal(t, true)
		err := New(Config{StackPath: stackPath, PassphraseStdin: true, Quiet: true}).Deploy(t.Context())
		if !errors.Is(err, ErrPassphraseStdinTerminal) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		installTerminal(t, false)
		setStdin(t, strings.NewReader(""))
		err := New(Config{StackPath: stackPath, PassphraseStdin: true, Quiet: true}).Deploy(t.Context())
		if err == nil || !strings.Contains(err.Error(), "stdin") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestServe_PassphraseStdin(t *testing.T) {
	lockedStoreHome(t, "DEMO_TOKEN", testPass)
	installTerminal(t, false)
	setStdin(t, strings.NewReader(testPass+"\n"))

	port, closeListener := occupiedPort(t)
	defer closeListener()
	ctrl := New(Config{Port: port, Foreground: true, Quiet: true, PassphraseStdin: true})
	ctrl.SetWebFS(func() (fs.FS, error) { return nil, nil })
	if err := ctrl.Serve(t.Context()); err == nil {
		t.Fatal("expected occupied-port error")
	}
	if ctrl.vaultStore == nil || ctrl.vaultStore.IsLocked() {
		t.Fatal("serve --passphrase-stdin left the store locked")
	}
	if got := ctrl.vaultStore.UnlockSource(); got != vault.UnlockSourceStdin {
		t.Fatalf("UnlockSource = %q", got)
	}
}

func TestServe_PassphraseStdinTerminalRefused(t *testing.T) {
	lockedStoreHome(t, "DEMO_TOKEN", testPass)
	installTerminal(t, true)
	ctrl := New(Config{Port: 1, Foreground: true, Quiet: true, PassphraseStdin: true})
	if err := ctrl.Serve(t.Context()); !errors.Is(err, ErrPassphraseStdinTerminal) {
		t.Fatalf("err = %v", err)
	}
}

func TestServe_NeverPrompts(t *testing.T) {
	lockedStoreHome(t, "DEMO_TOKEN", testPass)
	ft := installTerminal(t, true, []byte(testPass))
	port, closeListener := occupiedPort(t)
	defer closeListener()
	ctrl := New(Config{Port: port, Foreground: true, Quiet: true})
	ctrl.SetWebFS(func() (fs.FS, error) { return nil, nil })
	_ = ctrl.Serve(t.Context())
	if ft.reads.Load() != 0 {
		t.Fatal("serve prompted")
	}
}
