package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// readPassword prompts twice without echo on a terminal; otherwise it reads the first line of stdin.
// Cancelling ctx abandons the read, restores the terminal and returns ctx.Err().
func readPassword(ctx context.Context, stdin io.Reader, prompt io.Writer) (string, error) {
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return readTerminalPassword(ctx, int(f.Fd()), prompt)
	}
	line, err := readCancellable(ctx, func() (string, error) {
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if errors.Is(err, io.EOF) {
			err = nil
		}
		return line, err
	}, func() {})
	if err != nil {
		return "", err
	}
	if line = strings.TrimRight(line, "\r\n"); line == "" {
		return "", errors.New("no password given on stdin")
	}
	return line, nil
}

// readTerminalPassword reads and confirms a password with echo disabled on the terminal fd.
func readTerminalPassword(ctx context.Context, fd int, prompt io.Writer) (string, error) {
	state, err := term.GetState(fd)
	if err != nil {
		return "", err
	}
	hidden := func(label string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		fmt.Fprint(prompt, label)
		cancelled := false
		pw, err := readCancellable(ctx, func() (string, error) {
			b, err := term.ReadPassword(fd)
			return string(b), err
		}, func() {
			cancelled = true
			_ = term.Restore(fd, state)
			fmt.Fprintln(prompt)
		})
		if !cancelled {
			fmt.Fprintln(prompt)
		}
		return pw, err
	}
	a, err := hidden("Password: ")
	if err != nil {
		return "", err
	}
	b, err := hidden("Repeat password: ")
	if err != nil {
		return "", err
	}
	if a != b {
		return "", errors.New("passwords do not match")
	}
	return a, nil
}

// readCancellable runs read in a goroutine and returns its result or, once ctx is done, calls onCancel once and
// returns ctx.Err(). A cancelled read goroutine stays blocked until the process exits, which is acceptable for a CLI.
func readCancellable(ctx context.Context, read func() (string, error), onCancel func()) (string, error) {
	type result struct {
		s   string
		err error
	}
	done := make(chan result, 1)
	go func() {
		s, err := read()
		done <- result{s, err}
	}()
	select {
	case r := <-done:
		return r.s, r.err
	case <-ctx.Done():
		onCancel()
		return "", ctx.Err()
	}
}
