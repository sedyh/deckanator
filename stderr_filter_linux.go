//go:build linux

package main

import (
	"bufio"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// noisePatterns lists stderr lines from C libraries that are known to be
// benign and not worth showing to the user. Extend as new ones appear.
var noisePatterns = []string{
	// WebKit2GTK claims SIGUSR1 (which Go holds by default) for its JSC
	// garbage collector - we do not use SIGUSR1, so ignore the warning.
	"Overriding existing handler for signal",
	// JSC's option parser walks JSC_* env vars and complains about names
	// it does not know; JSC_SIGNAL_FOR_GC is read via getenv directly.
	"invalid option: JSC_",
}

// filterStderr replaces FD 2 with a pipe whose read end is drained in a
// goroutine. Known benign lines are dropped, everything else is forwarded
// to the original stderr. This catches output from C dependencies too,
// because they write to FD 2 directly, not through os.Stderr.
func filterStderr() {
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
	go drainStderr(r, orig)
}

func drainStderr(r *os.File, orig *os.File) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	for s.Scan() {
		line := s.Text()
		if isKnownNoise(line) {
			continue
		}
		orig.WriteString(line + "\n")
	}
}

func isKnownNoise(line string) bool {
	for _, p := range noisePatterns {
		if strings.Contains(line, p) {
			return true
		}
	}
	return false
}
