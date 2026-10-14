package reload

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
)

// ErrNoStackLoaded is returned by Plan when the handler has no loaded
// configuration, or no stack path, to compare against.
var ErrNoStackLoaded = errors.New("no stack loaded")

// loadOptions returns the loader options shared by reload and plan.
// Caller must hold h.mu. Vault and set lookups are read-only here.
func (h *Handler) loadOptions() []config.LoadOption {
	loadOpts := []config.LoadOption{config.WithQuietLoad()}
	if h.vault != nil {
		loadOpts = append(loadOpts, config.WithVault(h.vault))
	}
	if h.vaultSet != nil {
		loadOpts = append(loadOpts, config.WithVaultSets(h.vaultSet))
	}
	return loadOpts
}

// Plan compares the stack file at h.stackPath with the configuration the
// daemon loaded. It does not apply the candidate, run security preflight,
// or modify currentCfg. Store-backed image, url, and network details are
// reported without values.
func (h *Handler) Plan(ctx context.Context) (*config.PlanDiff, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if h.currentCfg == nil || h.stackPath == "" {
		return nil, ErrNoStackLoaded
	}

	candidate, err := config.LoadStack(h.stackPath, h.loadOptions()...)
	if err != nil {
		return nil, fmt.Errorf("loading candidate configuration: %w", err)
	}
	diff := config.ComputePlan(candidate, h.currentCfg)
	redactStoreBackedPlanDetails(diff, candidate, h.currentCfg)
	return diff, nil
}

// redactStoreBackedPlanDetails replaces value-bearing image, url, and
// network details when either side records that field as a store consumer.
// Other details, including literal image tags, stay as ComputePlan produced them.
func redactStoreBackedPlanDetails(diff *config.PlanDiff, candidate, current *config.Stack) {
	if diff == nil {
		return
	}
	for i := range diff.Items {
		item := &diff.Items[i]
		if item.Action != config.DiffChange {
			continue
		}
		fields, kind, ok := planRedactionFields(item.Kind)
		if !ok {
			continue
		}
		for j, detail := range item.Details {
			field, matched := storeBackedDetailField(detail, fields)
			if !matched {
				continue
			}
			if referenceConsumesField(candidate, kind, item.Name, field) || referenceConsumesField(current, kind, item.Name, field) {
				item.Details[j] = field + " changed"
			}
		}
	}
}

func planRedactionFields(kind string) ([]string, config.ReferenceKind, bool) {
	switch kind {
	case "mcp-server":
		return []string{"image", "url", "network"}, config.RefKindMCPServer, true
	case "resource":
		return []string{"image", "network"}, config.RefKindResource, true
	default:
		return nil, "", false
	}
}

func storeBackedDetailField(detail string, fields []string) (string, bool) {
	for _, field := range fields {
		if strings.HasPrefix(detail, field+": ") {
			return field, true
		}
	}
	return "", false
}

func referenceConsumesField(stack *config.Stack, kind config.ReferenceKind, name, field string) bool {
	if stack == nil {
		return false
	}
	for _, consumers := range stack.References {
		for _, c := range consumers {
			if c.Kind == kind && c.Name == name && c.Field == field {
				return true
			}
		}
	}
	return false
}
