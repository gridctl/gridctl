package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"

	"github.com/gridctl/gridctl/pkg/skillpins"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"
)

// skillPinResetter resets a removed skill's pin in this daemon's store and
// in every other stack pin file whose daemon is not running. This process's
// stack is reset in memory and then excluded from the disk pass, with no
// warning, whether or not it held the pin. Another stack whose daemon is
// running is left in place and named in a warning. A stack whose state file
// cannot be read is left in place. The returned error does not mean the
// registry delete failed.
func (s *Server) skillPinResetter() skills.SkillPinResetter {
	return func(ctx context.Context, name string) ([]string, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var reset []string
		var resetErr error
		if s.skillPinStore != nil {
			if _, ok := s.skillPinStore.Get(name); ok {
				if err := s.skillPinStore.Reset(name); err != nil {
					resetErr = err
				} else if s.stackName != "" {
					reset = append(reset, s.stackName)
				}
			}
		}
		stacks, err := skillpins.ListStacks()
		if err != nil {
			return reset, joinResetErr(resetErr, err)
		}
		skip := map[string]bool{}
		if s.stackName != "" {
			skip[s.stackName] = true
		}
		for _, stack := range stacks {
			if stack == s.stackName {
				continue
			}
			st, lerr := state.Load(stack)
			switch {
			case lerr != nil && os.IsNotExist(lerr):
				// No state file: the stack is stopped. Reset it on disk.
			case lerr == nil && state.IsRunning(st):
				skip[stack] = true
				// Stack is a pin-file basename. Name is the removed skill identifier.
				slog.Warn(fmt.Sprintf("skill pin left in place: stack %s has a running daemon; run 'gridctl skill pins reset %s --stack %s'", stack, name, stack), // #nosec G706 -- identifiers only; no pin content
					"skill", name, "stack", stack)
			case lerr != nil:
				skip[stack] = true
				slog.Warn("skill pin left in place: stack state unreadable", // #nosec G706 -- stack is a pin-file basename; name is the removed skill identifier
					"skill", name, "stack", stack, "category", stateLoadCategory(lerr))
			}
		}
		diskReset, _, derr := skillpins.ResetOnDisk(ctx, name, func(stack string) bool {
			return skip[stack]
		})
		reset = append(reset, diskReset...)
		sort.Strings(reset)
		return reset, joinResetErr(resetErr, derr)
	}
}

func joinResetErr(first, second error) error {
	switch {
	case first == nil:
		return second
	case second == nil:
		return first
	default:
		return errors.Join(first, second)
	}
}

// stateLoadCategory names a daemon-state read failure without the path.
func stateLoadCategory(err error) string {
	switch {
	case errors.Is(err, state.ErrNewerSchema):
		return "newer_schema"
	case os.IsPermission(err):
		return "permission"
	default:
		return "corrupt"
	}
}
