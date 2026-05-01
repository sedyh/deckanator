//go:build linux

// Package outfilter re-routes file descriptor 2 through a pipe so that
// known benign noise lines from linked C libraries can be dropped before
// they reach the terminal. It is a linux-only helper; on other platforms
// Install is a no-op.
package outfilter

import (
	"bufio"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// Patterns is the list of substrings that mark a stderr line as known
// benign noise. Extend as new sources of junk appear.
var Patterns = []string{
	// WebKit2GTK claims SIGUSR1 (which Go holds by default) for its JSC
	// garbage collector - we do not use SIGUSR1, so the warning is harmless.
	"Overriding existing handler for signal",
	// JSC's option parser walks JSC_* env vars and complains about names
	// it does not own; JSC_SIGNAL_FOR_GC is read via getenv directly.
	"invalid option: JSC_",
}

// Install replaces FD 2 with a pipe and spawns a drain goroutine that
// filters lines matching Patterns. Everything else is forwarded to the
// original stderr. Safe to call at most once at program start.
func Install() {
	r, w, err := os.Pipe()
	if err != nil {
		return
	}
	origFd, err := unix.Dup(int(os.Stderr.Fd()))
	if err != nil {
		return
	}
	orig := os.NewFile(uintptr(origFd), "/dev/stderr")
	if err := unix.Dup2(int(w.Fd()), int(os.Stderr.Fd())); err != nil {
		return
	}
	go drain(r, orig)
}

func drain(r, orig *os.File) {
	const maxLine = 1 << 20
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), maxLine)
	for s.Scan() {
		line := s.Text()
		if noise(line) {
			continue
		}
		_, _ = orig.WriteString(line + "\n")
	}
}

func noise(line string) bool {
	for _, p := range Patterns {
		if strings.Contains(line, p) {
			return true
		}
	}
	return false
}
