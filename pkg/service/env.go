package service

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gridctl/gridctl/pkg/vault"
)

// FilterEnvironment keeps a positive allowlist from environ, in this order:
// PATH, DOCKER_HOST, DOCKER_CONTEXT, CONTAINER_HOST, GRIDCTL_RUNTIME,
// GRIDCTL_NO_SKILL_UPDATE_CHECK, any GRIDCTL_EXPERIMENTAL_*, and
// SSH_AUTH_SOCK only when its cleaned value is under $XDG_RUNTIME_DIR,
// /run/user/, or <home>/.ssh/. GRIDCTL_HOME is never copied; the caller
// writes it from the resolved home.
//
// skipped lists a rejected SSH_AUTH_SOCK value (the path only). Every
// other variable is dropped silently. A kept key that is also in
// vault.InternalCredentialKeys is an error (defense in depth; the
// allowlist cannot produce one).
func FilterEnvironment(environ []string, home string) (kept []KV, skipped []string, err error) {
	values := map[string]string{}
	var experimental []string
	xdg := ""
	envHome := ""
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		switch key {
		case "XDG_RUNTIME_DIR":
			xdg = value
		case "HOME":
			envHome = value
		}
		values[key] = value
		if strings.HasPrefix(key, "GRIDCTL_EXPERIMENTAL_") {
			experimental = append(experimental, key)
		}
	}
	sort.Strings(experimental)

	ordered := []string{
		"PATH",
		"DOCKER_HOST",
		"DOCKER_CONTEXT",
		"CONTAINER_HOST",
		"GRIDCTL_RUNTIME",
		"GRIDCTL_NO_SKILL_UPDATE_CHECK",
	}
	ordered = append(ordered, experimental...)

	for _, key := range ordered {
		value, ok := values[key]
		if !ok {
			continue
		}
		kept = append(kept, KV{Key: key, Value: value})
	}
	if value, ok := values["SSH_AUTH_SOCK"]; ok {
		if sshSocketAllowed(value, xdg, home, envHome) {
			kept = append(kept, KV{Key: "SSH_AUTH_SOCK", Value: value})
		} else {
			skipped = append(skipped, value)
		}
	}
	if err := rejectInternalCredentials(kept); err != nil {
		return nil, nil, err
	}
	return kept, skipped, nil
}

// sshSocketAllowed accepts a socket only when it lives under a directory
// that dies with the user session or sits in the user's .ssh. A /tmp
// agent socket is not durable across reboot and must not be written.
func sshSocketAllowed(value, xdg, home, envHome string) bool {
	cleaned := filepath.Clean(value)
	if xdg != "" && pathInside(filepath.Clean(xdg), cleaned) {
		return true
	}
	if pathInside("/run/user", cleaned) {
		return true
	}
	for _, root := range []string{home, envHome} {
		if root == "" {
			continue
		}
		if pathInside(filepath.Join(root, ".ssh"), cleaned) {
			return true
		}
	}
	return false
}

func rejectInternalCredentials(kept []KV) error {
	denied := map[string]struct{}{}
	for _, key := range vault.InternalCredentialKeys() {
		denied[key] = struct{}{}
	}
	for _, kv := range kept {
		if _, ok := denied[kv.Key]; ok {
			return fmt.Errorf("refusing to keep internal credential %s", kv.Key)
		}
	}
	return nil
}

func sortKV(pairs []KV) {
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Key < pairs[j].Key
	})
}

// insertSorted inserts kv after the fixed GRIDCTL_HOME and PATH prefix,
// keeping the remaining keys sorted. PATH and GRIDCTL_HOME are not duplicated.
func insertSorted(env []KV, kv KV) []KV {
	for _, existing := range env {
		if existing.Key == kv.Key {
			return env
		}
	}
	prefix := 0
	for prefix < len(env) && (env[prefix].Key == "GRIDCTL_HOME" || env[prefix].Key == "PATH") {
		prefix++
	}
	rest := append([]KV(nil), env[prefix:]...)
	rest = append(rest, kv)
	sortKV(rest)
	out := append([]KV(nil), env[:prefix]...)
	return append(out, rest...)
}
