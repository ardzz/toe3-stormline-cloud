//go:build !linux && !darwin

package main

import "errors"

// isTerminal is false on platforms without termios support, so the password is read from stdin.
func isTerminal(int) bool { return false }

func disableEcho(int) (func(), error) {
	return nil, errors.New("hidden password input is not supported on this platform; pipe the password on stdin")
}
