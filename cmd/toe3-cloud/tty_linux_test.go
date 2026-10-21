package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// openPTY returns the master and slave ends of a new pseudo-terminal; both close at test cleanup.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("pseudo-terminals unavailable: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	if err := unix.IoctlSetPointerInt(int(m.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatalf("unlock pty: %v", err)
	}
	n, err := unix.IoctlGetInt(int(m.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatalf("pty number: %v", err)
	}
	s, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open pty slave: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return m, s
}

func echoOn(t *testing.T, f *os.File) bool {
	t.Helper()
	tio, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatalf("get termios: %v", err)
	}
	return tio.Lflag&unix.ECHO != 0
}

// waitEchoOff polls until the reader has disabled echo, failing after five seconds.
func waitEchoOff(t *testing.T, f *os.File) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); echoOn(t, f); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("echo was never disabled")
		}
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type readResult struct {
	pw  string
	err error
}

func startRead(ctx context.Context, slave *os.File, prompt *lockedBuffer) <-chan readResult {
	ch := make(chan readResult, 1)
	go func() {
		pw, err := readPassword(ctx, slave, prompt)
		ch <- readResult{pw, err}
	}()
	return ch
}

func awaitRead(t *testing.T, ch <-chan readResult) readResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("readPassword did not return")
		return readResult{}
	}
}

func TestTerminalPasswordHidesInputAndConfirms(t *testing.T) {
	master, slave := openPTY(t)
	if !echoOn(t, slave) {
		t.Fatal("new pty starts without echo")
	}
	var prompt lockedBuffer
	res := startRead(context.Background(), slave, &prompt)
	waitEchoOff(t, slave)
	// Both lines arrive while echo is off; canonical mode hands them to the two reads one at a time.
	if _, err := master.Write([]byte("correct horse battery\ncorrect horse battery\n")); err != nil {
		t.Fatal(err)
	}
	if r := awaitRead(t, res); r.err != nil || r.pw != "correct horse battery" {
		t.Fatalf("readPassword = %q, %v", r.pw, r.err)
	}
	if !echoOn(t, slave) {
		t.Fatal("echo not restored after reading")
	}
	if got := prompt.String(); !strings.Contains(got, "Password: ") || !strings.Contains(got, "Repeat password: ") {
		t.Fatalf("prompts = %q", got)
	}
}

func TestTerminalPasswordMismatch(t *testing.T) {
	master, slave := openPTY(t)
	var prompt lockedBuffer
	res := startRead(context.Background(), slave, &prompt)
	waitEchoOff(t, slave)
	if _, err := master.Write([]byte("first secret\nother secret\n")); err != nil {
		t.Fatal(err)
	}
	if r := awaitRead(t, res); r.err == nil || r.err.Error() != "passwords do not match" {
		t.Fatalf("readPassword err = %v, want mismatch", r.err)
	}
	if !echoOn(t, slave) {
		t.Fatal("echo not restored after mismatch")
	}
}

func TestTerminalPasswordCancelRestoresEcho(t *testing.T) {
	_, slave := openPTY(t)
	var prompt lockedBuffer
	ctx, cancel := context.WithCancel(context.Background())
	res := startRead(ctx, slave, &prompt)
	waitEchoOff(t, slave)
	cancel()
	if r := awaitRead(t, res); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("readPassword err = %v, want context.Canceled", r.err)
	}
	if !echoOn(t, slave) {
		t.Fatal("echo not restored after cancellation")
	}
	if strings.Contains(prompt.String(), "Repeat password") {
		t.Fatal("confirmation prompted after cancellation")
	}
}
