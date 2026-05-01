//go:build !linux

// Package outfilter is a no-op on non-Linux platforms, where the
// WebKit2GTK noise it targets does not exist.
package outfilter

// Install is a no-op outside of Linux.
func Install() {}
