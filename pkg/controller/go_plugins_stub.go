//go:build windows

package controller

import (
	"errors"

	"github.com/gridctl/gridctl/pkg/agent/skill"
)

// openAndRegisterGoSkill is the windows stub. The plugin package is
// not implemented on windows, so a Go skill that landed here can never
// load — the build path returns the same platform error at agent build
// time, but the daemon may still encounter an on-disk skill.so left
// behind by a cross-built artifact. Returning a clear error lets the
// loader log "go plugins unsupported on this platform" and skip.
func openAndRegisterGoSkill(_ string, _ *skill.Registry) error {
	return errors.New("go plugins are not supported on windows")
}
