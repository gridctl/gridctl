package config

import (
	"sort"
	"strings"
)

// VaultNeeds reports why a stack cannot start against a locked variable
// store. The list is sorted and deduplicated, and empty when a locked
// store is enough.
//
// It does not use UnresolvedRefs. ResolveVariable falls back to the
// process environment, secrets.sets is injected as an empty map when the
// store is locked, and credential_ref is left literal until clone time.
// Those three sites are invisible to an unresolved-ref check and still
// fail, or silently do nothing, once the service starts with a locked store.
func VaultNeeds(s *Stack) []string {
	if s == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var reasons []string
	add := func(reason string) {
		if reason == "" {
			return
		}
		if _, ok := seen[reason]; ok {
			return
		}
		seen[reason] = struct{}{}
		reasons = append(reasons, reason)
	}
	keys := make([]string, 0, len(s.References))
	for key := range s.References {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		add("variable ${var:" + key + "}")
	}
	if s.Secrets != nil {
		names := make([]string, 0, len(s.Secrets.Sets))
		for _, set := range s.Secrets.Sets {
			if set.Name != "" {
				names = append(names, set.Name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			add("secrets.sets " + name)
		}
	}
	for _, srv := range s.MCPServers {
		if srv.Source == nil || srv.Source.Auth == nil {
			continue
		}
		ref := srv.Source.Auth.CredentialRef
		if strings.Contains(ref, "${var:") || strings.Contains(ref, "${vault:") {
			add("credential_ref " + ref + " on " + srv.Name)
		}
	}
	sort.Strings(reasons)
	return reasons
}
