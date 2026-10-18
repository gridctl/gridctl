package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// headerFlags is a repeatable -require-header NAME=VALUE flag.
type headerFlags map[string]string

func (h *headerFlags) String() string { return "" }

func (h *headerFlags) Set(value string) error {
	name, want, ok := strings.Cut(value, "=")
	if !ok || name == "" {
		return fmt.Errorf("require-header must be NAME=VALUE")
	}
	if *h == nil {
		*h = map[string]string{}
	}
	(*h)[name] = want
	return nil
}

// requireHeaders records the header names seen on /mcp and returns 401 when
// a required header is missing or wrong. The body does not include values.
func requireHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		names := make([]string, 0, len(r.Header))
		for name := range r.Header {
			names = append(names, name)
		}
		sort.Strings(names)
		lastHeaderMu.Lock()
		lastHeaderNames = names
		lastHeaderMu.Unlock()
		for name, want := range requiredHeaders {
			if r.Header.Get(name) != want {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func handleLastHeaders(w http.ResponseWriter, _ *http.Request) {
	lastHeaderMu.Lock()
	names := append([]string(nil), lastHeaderNames...)
	lastHeaderMu.Unlock()
	if names == nil {
		names = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(names)
}
