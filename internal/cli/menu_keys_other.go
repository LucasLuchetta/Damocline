//go:build !windows

package cli

// On platforms without the Windows console event API, preserve the existing
// line-oriented numbered menu. The launcher and load engine remain portable.
func openHomeKeyboard() (homeKeyboard, bool) {
	return nil, false
}
