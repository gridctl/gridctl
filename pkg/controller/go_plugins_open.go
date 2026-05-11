//go:build !windows

package controller

import (
	"fmt"
	"plugin"

	"github.com/gridctl/gridctl/pkg/agent/skill"
)

// openAndRegisterGoSkill opens the compiled plugin at soPath, looks up
// the canonical `RegisterSkill(*skill.Registry) error` symbol, and
// invokes it against staging. Any failure — plugin.Open, symbol miss,
// wrong signature, or a non-nil error from RegisterSkill itself — is
// returned to the loader which logs at warn and moves on. The contract
// (symbol name + signature) is documented in docs/skills.md and in the
// Go scaffold so author copy-paste mistakes surface here, not at first
// CallTool.
func openAndRegisterGoSkill(soPath string, staging *skill.Registry) error {
	p, err := plugin.Open(soPath)
	if err != nil {
		return fmt.Errorf("plugin.Open: %w", err)
	}
	sym, err := p.Lookup("RegisterSkill")
	if err != nil {
		return fmt.Errorf("missing RegisterSkill symbol: %w", err)
	}
	register, ok := sym.(func(*skill.Registry) error)
	if !ok {
		return fmt.Errorf("RegisterSkill has wrong signature: want func(*skill.Registry) error, got %T", sym)
	}
	if err := register(staging); err != nil {
		return fmt.Errorf("RegisterSkill returned error: %w", err)
	}
	return nil
}
