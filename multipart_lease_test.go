package message

import (
	"errors"
	"io"
	"io/ioutil"
	"strings"
	"testing"
)

// leaseFixtureString is a two-part multipart/mixed message read from an
// in-memory stream. Both bodies fit in one segment so that reading half of
// them leaves the remainder buffered behind the boundary scanner.
const leaseFixtureString = "Content-Type: multipart/mixed; boundary=BOUND\r\n" +
	"\r\n" +
	"--BOUND\r\n" +
	"Content-Type: text/plain\r\n" +
	"\r\n" +
	"0123456789ABCDEF\r\n" +
	"--BOUND\r\n" +
	"Content-Type: text/plain\r\n" +
	"\r\n" +
	"FEDCBA9876543210\r\n" +
	"--BOUND--\r\n"

const leasePart1 = "0123456789ABCDEF"
const leasePart2 = "FEDCBA9876543210"

// closeTrackingReader wraps an in-memory reader to verify that the multipart
// parser borrows, but never closes, the underlying stream.
type closeTrackingReader struct {
	r     io.Reader
	close int
}

func (c *closeTrackingReader) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c *closeTrackingReader) Close() error {
	c.close++
	return nil
}

func leaseReaderFromFixture(t *testing.T, r io.Reader) MultipartReader {
	t.Helper()
	e, err := Read(r)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	mr := e.MultipartReader()
	if mr == nil {
		t.Fatalf("expected a multipart reader")
	}
	return mr
}

func halfReadAndAbandon(t *testing.T, mr MultipartReader, want string, i int) {
	t.Helper()
	lease, err := mr.LeasePart()
	if err != nil {
		t.Fatalf("part %d: LeasePart: %v", i, err)
	}
	if lease.Consumed() {
		t.Fatalf("part %d: fresh lease reported as consumed", i)
	}

	half := make([]byte, len(want)/2)
	if _, err := io.ReadFull(lease.Part().Body, half); err != nil {
		t.Fatalf("part %d: half read: %v", i, err)
	}
	if got := string(half); got != want[:len(want)/2] {
		t.Fatalf("part %d: first half = %q, want %q", i, got, want[:len(want)/2])
	}
	if lease.Consumed() {
		t.Fatalf("part %d: half-read lease reported as consumed", i)
	}
	if err := lease.Abandon(); err != nil {
		t.Fatalf("part %d: Abandon: %v", i, err)
	}
}

// TestLeasePart_HalfReadThenAbandon: for each of the two parts only the first
// half is read before giving up; after Abandon the next part content must
// still be parsed correctly.
func TestLeasePart_HalfReadThenAbandon(t *testing.T) {
	mr := leaseReaderFromFixture(t, strings.NewReader(leaseFixtureString))
	defer mr.Close()

	halfReadAndAbandon(t, mr, leasePart1, 0)
	halfReadAndAbandon(t, mr, leasePart2, 1)

	if _, err := mr.LeasePart(); err != io.EOF {
		t.Fatalf("LeasePart after last part = %v, want io.EOF", err)
	}
}

// TestLeasePart_AdvanceWithoutDrainRejected: advancing while the current part
// is only half read is refused with ErrPartNotConsumed; after consuming the
// rest, the next part is returned intact.
func TestLeasePart_AdvanceWithoutDrainRejected(t *testing.T) {
	mr := leaseReaderFromFixture(t, strings.NewReader(leaseFixtureString))
	defer mr.Close()

	first, err := mr.LeasePart()
	if err != nil {
		t.Fatalf("LeasePart: %v", err)
	}
	if _, err := io.ReadFull(first.Part().Body, make([]byte, 4)); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}

	if _, err := mr.LeasePart(); !errors.Is(err, ErrPartNotConsumed) {
		t.Fatalf("LeasePart on undrained part = %v, want ErrPartNotConsumed", err)
	}

	// The refused advance must not disturb the current lease.
	rest, err := ioutil.ReadAll(first.Part().Body)
	if err != nil {
		t.Fatalf("ReadAll after refused advance: %v", err)
	}
	if got := string(rest); got != leasePart1[4:] {
		t.Fatalf("remaining body = %q, want %q", got, leasePart1[4:])
	}
	if !first.Consumed() {
		t.Fatalf("lease not marked consumed after reading to EOF")
	}

	second, err := mr.LeasePart()
	if err != nil {
		t.Fatalf("LeasePart after full read: %v", err)
	}
	body, err := ioutil.ReadAll(second.Part().Body)
	if err != nil {
		t.Fatalf("ReadAll second: %v", err)
	}
	if string(body) != leasePart2 {
		t.Fatalf("second part = %q, want %q", body, leasePart2)
	}
}

// TestLeasePart_DoesNotCloseBorrowedStream: abandoning leases and closing the
// reader must not close the borrowed underlying stream.
func TestLeasePart_DoesNotCloseBorrowedStream(t *testing.T) {
	underlying := &closeTrackingReader{r: strings.NewReader(leaseFixtureString)}
	mr := leaseReaderFromFixture(t, underlying)

	for i := 0; i < 2; i++ {
		lease, err := mr.LeasePart()
		if err != nil {
			t.Fatalf("part %d: LeasePart: %v", i, err)
		}
		if _, err := io.CopyN(ioutil.Discard, lease.Part().Body, 3); err != nil {
			t.Fatalf("part %d: CopyN: %v", i, err)
		}
		if err := lease.Abandon(); err != nil {
			t.Fatalf("part %d: Abandon: %v", i, err)
		}
	}
	if err := mr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if underlying.close != 0 {
		t.Fatalf("underlying stream closed %d time(s), want 0", underlying.close)
	}
}

// TestLeasePart_FullConsumeAdvances: reading a part to io.EOF marks it
// consumed and allows advancing without an explicit Abandon.
func TestLeasePart_FullConsumeAdvances(t *testing.T) {
	mr := leaseReaderFromFixture(t, strings.NewReader(leaseFixtureString))
	defer mr.Close()

	first, err := mr.LeasePart()
	if err != nil {
		t.Fatalf("LeasePart: %v", err)
	}
	body, err := ioutil.ReadAll(first.Part().Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != leasePart1 {
		t.Fatalf("first part = %q, want %q", body, leasePart1)
	}
	if !first.Consumed() {
		t.Fatalf("part read to EOF not marked consumed")
	}

	second, err := mr.LeasePart()
	if err != nil {
		t.Fatalf("LeasePart after consumed part: %v", err)
	}
	if second.Consumed() {
		t.Fatalf("fresh lease reported as consumed")
	}
}

// TestLeasePart_AbandonIdempotent: Abandon after full consumption is harmless.
func TestLeasePart_AbandonIdempotent(t *testing.T) {
	mr := leaseReaderFromFixture(t, strings.NewReader(leaseFixtureString))
	defer mr.Close()

	first, err := mr.LeasePart()
	if err != nil {
		t.Fatalf("LeasePart: %v", err)
	}
	if _, err := ioutil.ReadAll(first.Part().Body); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if err := first.Abandon(); err != nil {
		t.Fatalf("Abandon after EOF: %v", err)
	}
	if err := first.Abandon(); err != nil {
		t.Fatalf("second Abandon: %v", err)
	}
	if _, err := mr.LeasePart(); err != nil {
		t.Fatalf("LeasePart after Abandon: %v", err)
	}
}

// TestLeasePart_inMemory runs the half-read/abandon scenario against the
// in-memory multipartBody implementation produced by NewMultipart.
func TestLeasePart_inMemory(t *testing.T) {
	var h1, h2, h Header
	h1.Set("Content-Type", "text/plain")
	h2.Set("Content-Type", "text/plain")
	h.Set("Content-Type", "multipart/mixed; boundary=BOUND")

	e1, _ := New(h1, strings.NewReader(leasePart1))
	e2, _ := New(h2, strings.NewReader(leasePart2))
	e, err := NewMultipart(h, []*Entity{e1, e2})
	if err != nil {
		t.Fatalf("NewMultipart: %v", err)
	}
	mr := e.MultipartReader()
	if mr == nil {
		t.Fatal("nil MultipartReader")
	}
	defer mr.Close()

	halfReadAndAbandon(t, mr, leasePart1, 0)
	halfReadAndAbandon(t, mr, leasePart2, 1)

	if _, err := mr.LeasePart(); err != io.EOF {
		t.Fatalf("LeasePart after last = %v, want io.EOF", err)
	}
}
