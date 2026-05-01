// Package errs contains tiny helpers for composing errors in defer
// chains without boilerplate.
package errs

import (
	"errors"
	"io"
	"os"
)

// Close closes c and merges any resulting error into *dest using
// errors.Join. A nil closer and an "already closed" error are ignored,
// so this is safe as a defer next to an explicit Close before return.
//
//	func Do(...) (e error) {
//	    c, err := Open(...)
//	    if err != nil { return err }
//	    defer errs.Close(&e, c)
//	    ...
//	}
func Close(dest *error, c io.Closer) {
	if c == nil {
		return
	}
	err := c.Close()
	if err == nil || errors.Is(err, os.ErrClosed) {
		return
	}
	*dest = errors.Join(*dest, err)
}

// DoSilentOnError runs fn only when *dest is non-nil and discards its
// result. It is meant for best-effort cleanup (removing a tmp file,
// rolling back a partial write) attached via defer to a function that
// returns a named error.
//
//	func Do(...) (e error) {
//	    tmp := create()
//	    defer errs.DoSilentOnError(&e, func() error { return os.Remove(tmp) })
//	    ...
//	}
func DoSilentOnError(dest *error, fn func() error) {
	if *dest == nil {
		return
	}
	_ = fn()
}

// Remove returns a cleanup callback that deletes path. Handy with
// DoSilentOnError to remove temporary files on a failed operation.
//
//	defer errs.DoSilentOnError(&e, errs.Remove(tmp))
func Remove(path string) func() error {
	return func() error { return os.Remove(path) }
}
