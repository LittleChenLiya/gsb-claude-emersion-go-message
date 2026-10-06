package textproto

import (
	"io"
	"io/ioutil"
	"strings"
	"testing"
)

const leaseTestBody = "--lease-boundary\r\n" +
	"Content-Type: text/plain\r\n" +
	"\r\n" +
	"first part body\r\n" +
	"--lease-boundary\r\n" +
	"Content-Type: text/plain\r\n" +
	"\r\n" +
	"second part body\r\n" +
	"--lease-boundary--\r\n"

func newLeaseTestReader(r io.Reader) *MultipartReader {
	return NewMultipartReader(r, "lease-boundary")
}

// Read only half of the first part, abandon it via Release, and check that
// the second part is still parsed correctly.
func TestMultipartLeasePartialReadThenRelease(t *testing.T) {
	r := newLeaseTestReader(strings.NewReader(leaseTestBody))

	l1, err := r.LeasePart()
	if err != nil {
		t.Fatalf("LeasePart() = %v", err)
	}

	// Read only half of the first part.
	half := len("first part body") / 2
	buf := make([]byte, half)
	if _, err := io.ReadFull(l1, buf); err != nil {
		t.Fatalf("ReadFull() = %v", err)
	}
	if string(buf) != "first p" {
		t.Errorf("half read = %q, want %q", buf, "first p")
	}
	if l1.FullyConsumed() {
		t.Error("FullyConsumed() = true after partial read, want false")
	}

	// Abandon the part: the remainder is drained, then the reader advances.
	if err := l1.Release(); err != nil {
		t.Fatalf("Release() = %v", err)
	}
	if !l1.Released() {
		t.Error("Released() = false after Release(), want true")
	}
	// The caller never consumed the part fully; draining on Release must
	// not change that record.
	if l1.FullyConsumed() {
		t.Error("FullyConsumed() = true after Release(), want false")
	}

	l2, err := r.LeasePart()
	if err != nil {
		t.Fatalf("LeasePart() after Release = %v", err)
	}
	b, err := ioutil.ReadAll(l2)
	if err != nil {
		t.Fatalf("ReadAll() = %v", err)
	}
	if string(b) != "second part body" {
		t.Errorf("second part body = %q, want %q", b, "second part body")
	}
	if !l2.FullyConsumed() {
		t.Error("FullyConsumed() = false after ReadAll, want true")
	}

	if _, err := r.LeasePart(); err != io.EOF {
		t.Errorf("LeasePart() at end = %v, want io.EOF", err)
	}
}

// Advancing while the current part has not been drained must be rejected.
func TestMultipartLeaseNotDrainedRejected(t *testing.T) {
	r := newLeaseTestReader(strings.NewReader(leaseTestBody))

	l1, err := r.LeasePart()
	if err != nil {
		t.Fatalf("LeasePart() = %v", err)
	}
	if _, err := l1.Read(make([]byte, 4)); err != nil {
		t.Fatalf("Read() = %v", err)
	}

	if _, err := r.LeasePart(); err != ErrPartNotDrained {
		t.Fatalf("LeasePart() with undrained part = %v, want ErrPartNotDrained", err)
	}

	// Fully consuming the part allows advancing again.
	if _, err := io.Copy(ioutil.Discard, l1); err != nil {
		t.Fatalf("drain = %v", err)
	}
	if !l1.FullyConsumed() {
		t.Error("FullyConsumed() = false after drain, want true")
	}
	if _, err := r.LeasePart(); err != nil {
		t.Fatalf("LeasePart() after full consumption = %v", err)
	}
}

// Leasing must not close the borrowed underlying stream.
func TestMultipartLeaseDoesNotCloseUnderlying(t *testing.T) {
	rc := &trackReadCloser{Reader: strings.NewReader(leaseTestBody)}
	r := newLeaseTestReader(rc)

	for {
		l, err := r.LeasePart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("LeasePart() = %v", err)
		}
		// Abandon every part without reading it.
		if err := l.Release(); err != nil {
			t.Fatalf("Release() = %v", err)
		}
	}

	if rc.closed {
		t.Error("underlying stream was closed by leasing")
	}
}

// Release is idempotent.
func TestMultipartLeaseReleaseIdempotent(t *testing.T) {
	r := newLeaseTestReader(strings.NewReader(leaseTestBody))
	l, err := r.LeasePart()
	if err != nil {
		t.Fatalf("LeasePart() = %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("first Release() = %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("second Release() = %v", err)
	}
}

type trackReadCloser struct {
	io.Reader
	closed bool
}

func (rc *trackReadCloser) Close() error {
	rc.closed = true
	return nil
}
