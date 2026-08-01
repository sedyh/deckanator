//go:build linux

// Package actionset passively detects which Steam Input action set is
// active on a Deck desktop session. Steam keeps a virtual X-Box pad
// device around in both sets, but only feeds it in the gamepad set -
// at rest it still ticks with stick noise (~150 events/s), while in the
// desktop set it goes completely silent (buttons are routed to the
// virtual keyboard instead). Watching that flow needs no user input.
package actionset

import (
	"context"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Silence longer than this on the virtual pad means the desktop set.
// Measured on a Deck at rest (screen dimming included): ~740 events/s
// with a worst-case gap of 20ms, so a second is a 50x margin; the
// notice appearing within about a second is fast enough.
const silence = time.Second

const rescanEvery = 5 * time.Second

// Virtual pad device names Steam has been seen using.
var padNames = []string{"x-box", "xbox", "steam virtual"}

func deviceName(fd int) string {
	buf := make([]byte, 256)
	// EVIOCGNAME(len): _IOC(_IOC_READ, 'E', 0x06, len)
	req := uintptr(2)<<30 | uintptr(len(buf))<<16 | uintptr('E')<<8 | 0x06
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return ""
	}
	if i := strings.IndexByte(string(buf), 0); i >= 0 {
		return string(buf[:i])
	}
	return string(buf)
}

func findVirtualPad() int {
	paths, _ := filepath.Glob("/dev/input/event*")
	for _, p := range paths {
		fd, err := syscall.Open(p, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			continue
		}
		name := strings.ToLower(deviceName(fd))
		for _, want := range padNames {
			if strings.Contains(name, want) {
				return fd
			}
		}
		_ = syscall.Close(fd)
	}
	return -1
}

// Watch reports the active set through emit: true for the gamepad set,
// false for the desktop set. The first report waits until the state is
// established: an event tick settles it as gamepad immediately, a full
// silence window settles it as desktop. Runs until ctx ends.
func Watch(ctx context.Context, emit func(gamepadSet bool)) {
	go func() {
		fd := -1
		defer func() {
			if fd >= 0 {
				_ = syscall.Close(fd)
			}
		}()

		buf := make([]byte, 4096)
		var lastTick, openedAt time.Time
		var lastScan time.Time
		established := false
		current := false

		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			if fd < 0 {
				if time.Since(lastScan) < rescanEvery {
					continue
				}
				lastScan = time.Now()
				fd = findVirtualPad()
				if fd < 0 {
					continue
				}
				openedAt = time.Now()
				lastTick = openedAt
			}

			ticked := false
			for {
				n, err := syscall.Read(fd, buf)
				if n > 0 {
					ticked = true
					continue
				}
				if err == syscall.EAGAIN || n == 0 {
					break
				}
				// Device gone (Steam restart): drop and rescan.
				_ = syscall.Close(fd)
				fd = -1
				break
			}
			if fd < 0 {
				continue
			}
			if ticked {
				lastTick = time.Now()
			}

			if !established {
				if ticked {
					established = true
					current = true
					emit(true)
				} else if time.Since(openedAt) >= silence {
					established = true
					current = false
					emit(false)
				}
				continue
			}

			gamepadSet := time.Since(lastTick) < silence
			if gamepadSet != current {
				current = gamepadSet
				emit(gamepadSet)
			}
		}
	}()
}

// Supported is a build-tag hook: only Linux builds can watch.
func Supported() bool { return true }
