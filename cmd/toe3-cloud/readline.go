package main

import (
	"context"
	"errors"
	"io"
	"strings"
)

// readLine reads one line byte by byte, so nothing after the newline is consumed, and drops the line ending.
// It returns io.EOF only when no bytes were read.
func readLine(r io.Reader) (string, error) {
	var line []byte
	var b [1]byte
	for {
		n, err := r.Read(b[:])
		if n == 1 {
			if b[0] == '\n' {
				break
			}
			line = append(line, b[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				break
			}
			return "", err
		}
	}
	return strings.TrimRight(string(line), "\r"), nil
}

// readCancellable runs read in a goroutine and returns its result, or ctx.Err() once ctx is done. A cancelled read
// goroutine stays blocked until the process exits, which is acceptable for a CLI.
func readCancellable(ctx context.Context, read func() (string, error)) (string, error) {
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
		return "", ctx.Err()
	}
}
