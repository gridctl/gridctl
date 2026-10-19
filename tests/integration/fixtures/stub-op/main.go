// Stub op CLI. Validates read --no-newline op://... and prints a fixed value.
package main

import (
	"fmt"
	"os"
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
	fmt.Print("op-secret-value")
}
