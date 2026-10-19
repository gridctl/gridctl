// Stub op CLI. Validates read --no-newline op://... and prints a fixed value.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 4 || os.Args[1] != "read" || os.Args[2] != "--no-newline" || !strings.HasPrefix(os.Args[3], "op://") {
		fmt.Fprintf(os.Stderr, "usage: op read --no-newline op://vault/item/field\n")
		os.Exit(2)
	}
	path := strings.TrimPrefix(os.Args[3], "op://")
	parts := strings.Split(path, "/")
	if len(parts) != 3 && len(parts) != 4 {
		fmt.Fprintf(os.Stderr, "invalid reference\n")
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "OP_SERVICE_ACCOUNT_TOKEN=%s\n", os.Getenv("OP_SERVICE_ACCOUNT_TOKEN"))
	fmt.Fprintf(os.Stderr, "OP_CONNECT_HOST=%s\n", os.Getenv("OP_CONNECT_HOST"))
	fmt.Fprintf(os.Stderr, "UNRELATED=%s\n", os.Getenv("UNRELATED"))
	// The report sits beside the binary. An env var would have to be on the
	// allowlist, which would hide the leak this file exists to catch.
	report := filepath.Join(filepath.Dir(os.Args[0]), "op-report")
	var b strings.Builder
	b.WriteString("argv=" + strings.Join(os.Args[1:], " ") + "\n")
	for _, key := range []string{"PATH", "HOME", "XDG_CONFIG_HOME", "OP_SERVICE_ACCOUNT_TOKEN", "OP_CONNECT_HOST", "OP_CONNECT_TOKEN", "OP_ACCOUNT", "UNRELATED", "GRIDCTL_HOME", "GRIDCTL_VAULT_PASSPHRASE"} {
		if value, ok := os.LookupEnv(key); ok {
			fmt.Fprintf(&b, "%s=%s\n", key, value)
		} else {
			fmt.Fprintf(&b, "%s=<unset>\n", key)
		}
	}
	if err := os.WriteFile(report, []byte(b.String()), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "report: %v\n", err)
		os.Exit(1)
	}
	fmt.Print("op-secret-value")
}
