package controller

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/vault"

	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

const (
	// PassphrasePrompt is written to stderr before a hidden passphrase read.
	PassphrasePrompt = "Variable store is locked. Passphrase: " // #nosec G101 -- prompt text, not a secret value

	// passphraseStdinLimit caps a --passphrase-stdin line.
	passphraseStdinLimit = 4096

	// passphraseAttempts is the number of prompt attempts before failing.
	passphraseAttempts = 3
)

var (
	// ErrPassphraseCancelled is returned when the prompt sees EOF or the
	// context is cancelled (ctrl-C under signal.NotifyContext).
	ErrPassphraseCancelled = errors.New("passphrase entry cancelled")

	// ErrPassphraseStdinTerminal is returned when --passphrase-stdin is set
	// and stdin is a terminal.
	ErrPassphraseStdinTerminal = errors.New("--passphrase-stdin requires piped input")
)

// Test seams. The terminal functions are package variables so unit tests can
// exercise the prompt and its cancel path without a pseudo-terminal.
var (
	stdinReadTimeout           = 10 * time.Second
	stdinSource      io.Reader = os.Stdin
	promptIn                   = os.Stdin
	promptOut        io.Writer = os.Stderr
	stdinIsTerminal            = func() bool { return isTerminal(os.Stdin) }
	termGetState               = term.GetState
	termRestore                = term.Restore
	termReadPassword           = term.ReadPassword
)

// isTerminal reports whether f is a terminal that term.ReadPassword can
// read. A Cygwin or MSYS pseudo-terminal is a Windows pipe, so it does not
// count.
func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd())
}

// readPassphraseStdin reads one line of at most passphraseStdinLimit bytes
// from r, trimming a trailing "\n" and "\r". The read runs in a goroutine and
// the caller waits on it, stdinReadTimeout, and ctx: a deadline on os.Stdin
// does not work for a blocking pipe, so the goroutine is abandoned on timeout
// or cancel and the process is expected to exit.
func readPassphraseStdin(ctx context.Context, r io.Reader) (string, error) {
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		br := bufio.NewReader(io.LimitReader(r, passphraseStdinLimit+1))
		line, err := br.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			done <- result{err: fmt.Errorf("reading passphrase from stdin: %w", err)}
			return
		}
		done <- result{line: line}
	}()

	timer := time.NewTimer(stdinReadTimeout)
	defer timer.Stop()
	select {
	case res := <-done:
		if res.err != nil {
			return "", res.err
		}
		line := strings.TrimSuffix(res.line, "\n")
		line = strings.TrimSuffix(line, "\r")
		if len(line) > passphraseStdinLimit {
			return "", fmt.Errorf("passphrase on stdin exceeds %d bytes", passphraseStdinLimit)
		}
		if line == "" {
			return "", errors.New("no passphrase on stdin")
		}
		return line, nil
	case <-timer.C:
		return "", fmt.Errorf("no passphrase arrived on stdin within %s", stdinReadTimeout)
	case <-ctx.Done():
		return "", fmt.Errorf("reading passphrase from stdin: %w", ctx.Err())
	}
}

// PromptPassphrase prompts on out for the passphrase, reads it hidden from
// the terminal in, and unlocks store with it. A wrong passphrase prints
// "wrong passphrase" and prompts again, up to three attempts. EOF or a
// cancelled ctx restores the terminal state, writes a newline, and returns
// ErrPassphraseCancelled. The returned passphrase is the one that unlocked
// the store; callers must not print or persist it.
func PromptPassphrase(ctx context.Context, store *vault.Store, in *os.File, out io.Writer) (string, error) {
	for attempt := 1; attempt <= passphraseAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", ErrPassphraseCancelled
		}
		fmt.Fprint(out, PassphrasePrompt)
		raw, err := ReadHidden(ctx, in, out)
		if err != nil {
			return "", err
		}
		pass := string(raw)
		err = store.Unlock(pass)
		if err == nil {
			return pass, nil
		}
		if !errors.Is(err, vault.ErrWrongPassphrase) {
			return "", fmt.Errorf("unlocking vault: %w", err)
		}
		fmt.Fprintln(out, "wrong passphrase")
	}
	return "", errors.New("unlocking vault: wrong passphrase, three attempts used")
}

// ReadHidden reads one hidden line from the terminal in and writes a newline
// to out afterward. ReadPassword runs in a goroutine so a cancelled ctx
// returns at once; the terminal state saved before the read is restored on
// that path because ReadPassword's own restore only runs when its read
// returns. EOF and cancellation return ErrPassphraseCancelled.
func ReadHidden(ctx context.Context, in *os.File, out io.Writer) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, ErrPassphraseCancelled
	}
	fd := int(in.Fd()) // #nosec G115 -- file descriptors fit in int
	saved, stateErr := termGetState(fd)
	type result struct {
		raw []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		raw, err := termReadPassword(fd)
		done <- result{raw: raw, err: err}
	}()
	select {
	case res := <-done:
		fmt.Fprintln(out)
		if res.err != nil {
			if errors.Is(res.err, io.EOF) {
				return nil, ErrPassphraseCancelled
			}
			return nil, fmt.Errorf("reading passphrase: %w", res.err)
		}
		return res.raw, nil
	case <-ctx.Done():
		if stateErr == nil && saved != nil {
			_ = termRestore(fd, saved)
		}
		fmt.Fprintln(out)
		return nil, ErrPassphraseCancelled
	}
}

// acquirePassphrase runs after unlockFromEnvironment. With PassphraseStdin
// it reads one line from stdin (refusing a terminal) and unlocks a store the
// environment left locked; otherwise, when allowPrompt is set, this is not
// the daemon child, stdin is a terminal, the store is locked, and needsStore
// reports true, it prompts. It returns the passphrase that unlocked the
// store so a daemon parent can forward it to its child, or "" when nothing
// was obtained.
func (sc *StackController) acquirePassphrase(ctx context.Context, store *vault.Store, allowPrompt bool, needsStore func() bool) (string, error) {
	cfg := sc.config
	if cfg.PassphraseStdin {
		if stdinIsTerminal() {
			return "", ErrPassphraseStdinTerminal
		}
		line, err := readPassphraseStdin(ctx, stdinSource)
		if err != nil {
			return "", err
		}
		// The environment takes precedence; the line is read and dropped
		// so a wrapper writing it does not block.
		if store == nil || !store.IsLocked() {
			return "", nil
		}
		if err := store.Unlock(line); err != nil {
			if errors.Is(err, vault.ErrWrongPassphrase) {
				return "", errors.New("unlocking vault: passphrase from stdin rejected")
			}
			return "", fmt.Errorf("unlocking vault: %w", err)
		}
		recordUnlock(store, vault.UnlockSourceStdin, line)
		return line, nil
	}
	if !allowPrompt || cfg.DaemonChild || store == nil || !store.IsLocked() || !stdinIsTerminal() {
		return "", nil
	}
	if needsStore != nil && !needsStore() {
		return "", nil
	}
	pass, err := PromptPassphrase(ctx, store, promptIn, promptOut)
	if err != nil {
		return "", err
	}
	recordUnlock(store, vault.UnlockSourceStdin, pass)
	return pass, nil
}

// recordUnlock records how store was unlocked and registers the passphrase
// for log redaction.
func recordUnlock(store *vault.Store, source, pass string) {
	store.SetUnlockSource(source)
	logging.RegisterSensitiveValues(pass)
}

// stackNeedsStore reports whether the stack at cfg.StackPath references the
// variable store, loading it quietly without a store and with the same
// override selection. A load failure reports false so the ordinary load
// produces the error.
func stackNeedsStore(cfg Config) bool {
	opts := append([]config.LoadOption{config.WithQuietLoad()}, overrideLoadOptions(cfg)...)
	stack, err := config.LoadStack(cfg.StackPath, opts...)
	if err != nil {
		return false
	}
	return len(config.VaultNeeds(stack)) > 0
}
