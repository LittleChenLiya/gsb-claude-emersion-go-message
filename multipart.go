package message

import (
	"errors"
	"io"
	"io/ioutil"

	"github.com/emersion/go-message/textproto"
)

// ErrPartNotConsumed is returned by MultipartReader.LeasePart when the
// previously leased part has neither been read completely nor abandoned.
// The caller must either read the part's body until io.EOF or call
// PartLease.Abandon before the next part can be leased.
var ErrPartNotConsumed = errors.New("message: previous part has not been fully consumed; call PartLease.Abandon first")

// PartLease is an exclusive lease on a single part returned by
// MultipartReader.LeasePart. The lease owns the sequential read position of
// the multipart stream: no other part can be leased until the current one is
// released, either by reading its body until io.EOF or by calling Abandon.
//
// A lease does not own the underlying stream: it never closes the reader from
// which the multipart body is being parsed.
type PartLease interface {
	// Part returns the leased part. The returned pointer is valid until the
	// lease is released (its body read to io.EOF or Abandon is called).
	Part() *Entity

	// Consumed reports whether the part body has been read completely. It
	// becomes true once reading Entity.Body returns io.EOF.
	Consumed() bool

	// Abandon gives up the lease without consuming the rest of the part: the
	// remaining body bytes are drained to the discard writer so that the
	// stream is positioned at the next boundary, and the lease is released.
	// It returns any error encountered while draining.
	Abandon() error
}

// MultipartReader is an iterator over parts in a MIME multipart body.
type MultipartReader interface {
	io.Closer

	// NextPart returns the next part in the multipart or an error. When there are
	// no more parts, the error io.EOF is returned.
	//
	// Entity.Body must be read completely before the next call to NextPart,
	// otherwise it will be discarded.
	NextPart() (*Entity, error)

	// LeasePart returns an exclusive lease on the next part in the multipart
	// or an error. When there are no more parts, the error io.EOF is returned.
	//
	// Unlike NextPart, LeasePart does not silently discard an unfinished part:
	// the caller must either read the leased part body until io.EOF or call
	// PartLease.Abandon before the next LeasePart call. Otherwise
	// ErrPartNotConsumed is returned.
	LeasePart() (PartLease, error)
}

// leasedEntityBody wraps a part body so that reaching io.EOF marks the lease
// as fully consumed. While draining (after Abandon), the EOF does not count
// as consumption by the caller.
type leasedEntityBody struct {
	r        io.Reader
	lease    *leasedPart
	draining bool
}

func (b *leasedEntityBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err == io.EOF && !b.draining {
		b.lease.consumed = true
	}
	return n, err
}

// leasedPart is the shared PartLease implementation used both by the
// streaming multipartReader (borrowed boundary parsing) and by the in-memory
// multipartBody.
type leasedPart struct {
	part      *Entity
	body      *leasedEntityBody
	consumed  bool
	abandoned bool
}

func (l *leasedPart) Part() *Entity { return l.part }

func (l *leasedPart) Consumed() bool { return l.consumed }

func (l *leasedPart) Abandon() error {
	if l.consumed || l.abandoned {
		return nil
	}
	l.body.draining = true
	_, err := io.Copy(ioutil.Discard, l.body)
	l.body.draining = false
	if err != nil {
		return err
	}
	l.abandoned = true
	return nil
}

// newLeasedPart wraps the entity body so that reading it to io.EOF records
// the part as fully consumed. Draining goes through the (possibly decoded)
// entity body, so it follows the same boundary parsing as ordinary reading.
func newLeasedPart(part *Entity) *leasedPart {
	l := &leasedPart{part: part}
	l.body = &leasedEntityBody{r: part.Body, lease: l}
	l.part.Body = l.body
	return l
}

// readyToAdvance reports whether the lease has been released.
func (l *leasedPart) readyToAdvance() bool { return l == nil || l.consumed || l.abandoned }

type multipartReader struct {
	r     *textproto.MultipartReader
	lease *leasedPart
}

// NextPart implements MultipartReader.
func (r *multipartReader) NextPart() (*Entity, error) {
	// Preserve the historical semantics: an unfinished part is discarded.
	if r.lease != nil {
		if err := r.lease.Abandon(); err != nil {
			return nil, err
		}
		r.lease = nil
	}
	p, err := r.r.NextPart()
	if err != nil {
		return nil, err
	}
	return New(Header{p.Header}, p)
}

// LeasePart implements MultipartReader.
func (r *multipartReader) LeasePart() (PartLease, error) {
	if !r.lease.readyToAdvance() {
		return nil, ErrPartNotConsumed
	}
	r.lease = nil
	p, err := r.r.NextPart()
	if err != nil {
		return nil, err
	}
	part, _ := New(Header{p.Header}, p)
	r.lease = newLeasedPart(part)
	return r.lease, nil
}

// Close implements io.Closer.
func (r *multipartReader) Close() error {
	return nil
}

type multipartBody struct {
	header Header
	parts  []*Entity

	r *io.PipeReader
	w *Writer

	i     int
	lease *leasedPart
}

// Read implements io.Reader.
func (m *multipartBody) Read(p []byte) (n int, err error) {
	if m.r == nil {
		r, w := io.Pipe()
		m.r = r

		var err error
		m.w, err = createWriter(w, &m.header)
		if err != nil {
			return 0, err
		}

		// Prevent calls to NextPart to succeed
		m.i = len(m.parts)

		go func() {
			if err := m.writeBodyTo(m.w); err != nil {
				w.CloseWithError(err)
				return
			}

			if err := m.w.Close(); err != nil {
				w.CloseWithError(err)
				return
			}

			w.Close()
		}()
	}

	return m.r.Read(p)
}

// Close implements io.Closer.
func (m *multipartBody) Close() error {
	if m.r != nil {
		m.r.Close()
	}
	return nil
}

// NextPart implements MultipartReader.
func (m *multipartBody) NextPart() (*Entity, error) {
	// Preserve the historical semantics: an unfinished part is discarded.
	if m.lease != nil {
		if err := m.lease.Abandon(); err != nil {
			return nil, err
		}
		m.lease = nil
	}
	if m.i >= len(m.parts) {
		return nil, io.EOF
	}

	part := m.parts[m.i]
	m.i++
	return part, nil
}

// LeasePart implements MultipartReader.
func (m *multipartBody) LeasePart() (PartLease, error) {
	if !m.lease.readyToAdvance() {
		return nil, ErrPartNotConsumed
	}
	m.lease = nil
	if m.i >= len(m.parts) {
		return nil, io.EOF
	}
	part := m.parts[m.i]
	m.i++
	m.lease = newLeasedPart(part)
	return m.lease, nil
}

func (m *multipartBody) writeBodyTo(w *Writer) error {
	for _, p := range m.parts {
		pw, err := w.CreatePart(p.Header)
		if err != nil {
			return err
		}

		if err := p.writeBodyTo(pw); err != nil {
			return err
		}
		if err := pw.Close(); err != nil {
			return err
		}
	}
	return nil
}
