//go:build windows

package state

// FindOrphan is a no-op on Windows; gridctl daemon mode is POSIX-only.
func FindOrphan(port int) (int, bool, error) {
	return 0, false, nil
}
