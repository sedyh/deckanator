//go:build !linux

package actionset

import "context"

// Watch is a no-op off Linux: there is no Steam virtual pad to observe.
func Watch(ctx context.Context, emit func(gamepadSet bool)) {}

// Supported is a build-tag hook: only Linux builds can watch.
func Supported() bool { return false }
