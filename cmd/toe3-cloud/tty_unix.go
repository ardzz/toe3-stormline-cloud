//go:build linux || darwin

package main

import "golang.org/x/sys/unix"

// isTerminal reports whether fd is a terminal.
func isTerminal(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	return err == nil
}

// disableEcho turns off echo on terminal fd, keeping line editing and signals, and returns a function that restores
// the previous settings.
func disableEcho(fd int) (restore func(), err error) {
	old, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return nil, err
	}
	t := *old
	t.Lflag &^= unix.ECHO
	t.Lflag |= unix.ICANON | unix.ISIG
	t.Iflag |= unix.ICRNL
	if err := unix.IoctlSetTermios(fd, ioctlSetTermios, &t); err != nil {
		return nil, err
	}
	return func() { _ = unix.IoctlSetTermios(fd, ioctlSetTermios, old) }, nil
}
