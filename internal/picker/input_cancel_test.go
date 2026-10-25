//go:build darwin || linux

package picker

import (
	"io"
	"os"
	"testing"
	"time"
)

func TestKeyReaderCloseCancelsOutstandingRead(t *testing.T) {
	in, out, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()
	keys, err := newKeyReader(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := out.WriteString("\x1b"); err != nil {
		keys.close()
		t.Fatal(err)
	}
	if got, err := keys.next(); err != nil || got != "\x1b" {
		keys.close()
		t.Fatalf("Escape = %q, %v", got, err)
	}
	closed := make(chan struct{})
	go func() {
		keys.close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("closing picker did not cancel pending terminal read")
	}
	// The wrapper must leave the original input open and stop reading it.
	if _, err := out.WriteString("x"); err != nil {
		t.Fatal(err)
	}
	var buf [1]byte
	if _, err := io.ReadFull(in, buf[:]); err != nil || buf[0] != 'x' {
		t.Fatalf("input after picker closed = %q, %v; want x, nil", buf, err)
	}
}
