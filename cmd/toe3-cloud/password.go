package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// readPassword prompts twice with echo disabled on a terminal; otherwise it reads the first line of stdin.
// Cancelling ctx abandons the read, restores the terminal and returns ctx.Err().
func readPassword(ctx context.Context, stdin io.Reader, prompt io.Writer) (string, error) {
	if f, ok := stdin.(*os.File); ok && isTerminal(int(f.Fd())) {
		return readTerminalPassword(ctx, f, prompt)
	}
	line, err := readCancellable(ctx, func() (string, error) { return readLine(stdin) })
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err != nil {
		return "", err
	}
	if line == "" {
		return "", errors.New("no password given on stdin")
	}
	return line, nil
}

// readTerminalPassword reads and confirms a password on terminal f.
func readTerminalPassword(ctx context.Context, f *os.File, prompt io.Writer) (string, error) {
	a, err := readHidden(ctx, f, prompt, "Password: ")
	if err != nil {
		return "", err
	}
	b, err := readHidden(ctx, f, prompt, "Repeat password: ")
	if err != nil {
		return "", err
	}
	if a != b {
		return "", errors.New("passwords do not match")
	}
	return a, nil
}

// readHidden reads one line with echo off. Echo is disabled and restored on this goroutine, never by the reader,
// so an abandoned read cannot leave the terminal silent.
func readHidden(ctx context.Context, f *os.File, prompt io.Writer, label string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	restore, err := disableEcho(int(f.Fd()))
	if err != nil {
		return "", err
	}
	defer restore()
	fmt.Fprint(prompt, label)
	line, err := readCancellable(ctx, func() (string, error) { return readLine(f) })
	fmt.Fprintln(prompt)
	return line, err
}
