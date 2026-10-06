package message

import (
	"mime"

	"github.com/emersion/go-message/textproto"
)

func parseHeaderWithParams(s string) (f string, params map[string]string, err error) {
	f, params, err = mime.ParseMediaType(s)
	if err != nil {
		return s, nil, err
	}
	for k, v := range params {
		params[k], _ = decodeHeader(v)
	}
	return
}

func formatHeaderWithParams(f string, params map[string]string) string {
	encParams := make(map[string]string)
	for k, v := range params {
		encParams[k] = encodeHeader(v)
	}
	return mime.FormatMediaType(f, encParams)
}

// HeaderFields iterates over header fields.
type HeaderFields interface {
	textproto.HeaderFields

	// Text parses the value of the current field as plaintext. The field
	// charset is decoded to UTF-8. If the header field's charset is unknown,
	// the raw field value is returned and the error verifies IsUnknownCharset.
	Text() (string, error)

	headerFields()
}

type headerFields struct {
	textproto.HeaderFields
}

func (*headerFields) headerFields() {}

func (hf *headerFields) Text() (string, error) {
	return decodeHeader(hf.Value())
}

// A Header represents the key-value pairs in a message header.
type Header struct {
	textproto.Header
}

// HeaderFromMap creates a header from a map of header fields.
//
// This function is provided for interoperability with the standard library.
// If possible, ReadHeader should be used instead to avoid loosing information.
// The map representation looses the ordering of the fields, the capitalization
// of the header keys, and the whitespace of the original header.
func HeaderFromMap(m map[string][]string) Header {
	return Header{textproto.HeaderFromMap(m)}
}

// ContentType parses the Content-Type header field.
//
// If no Content-Type is specified, it returns "text/plain".
func (h *Header) ContentType() (t string, params map[string]string, err error) {
	v := h.Get("Content-Type")
	if v == "" {
		return "text/plain", nil, nil
	}
	return parseHeaderWithParams(v)
}

// SetContentType formats the Content-Type header field.
func (h *Header) SetContentType(t string, params map[string]string) {
	h.Set("Content-Type", formatHeaderWithParams(t, params))
}

// ContentDisposition parses the Content-Disposition header field, as defined in
// RFC 2183.
func (h *Header) ContentDisposition() (disp string, params map[string]string, err error) {
	return parseHeaderWithParams(h.Get("Content-Disposition"))
}

// SetContentDisposition formats the Content-Disposition header field, as
// defined in RFC 2183.
func (h *Header) SetContentDisposition(disp string, params map[string]string) {
	h.Set("Content-Disposition", formatHeaderWithParams(disp, params))
}

// Text parses a plaintext header field. The field charset is automatically
// decoded to UTF-8. If the header field's charset is unknown, the raw field
// value is returned and the error verifies IsUnknownCharset.
func (h *Header) Text(k string) (string, error) {
	return decodeHeader(h.Get(k))
}

// SetText sets a plaintext header field.
func (h *Header) SetText(k, v string) {
	h.Set(k, encodeHeader(v))
}

// Copy creates a stand-alone copy of the header.
func (h *Header) Copy() Header {
	return Header{h.Header.Copy()}
}

// Fields iterates over all the header fields.
//
// The header may not be mutated while iterating, except using HeaderFields.Del.
func (h *Header) Fields() HeaderFields {
	return &headerFields{h.Header.Fields()}
}

// FieldsByKey iterates over all fields having the specified key.
//
// The header may not be mutated while iterating, except using HeaderFields.Del.
func (h *Header) FieldsByKey(k string) HeaderFields {
	return &headerFields{h.Header.FieldsByKey(k)}
}

// An Entry is a single header field, providing a dual view of the field: the
// raw bytes as they appeared in the message, and the decoded value.
//
// Entries are a read-only view: they cannot be used to modify the header.
type Entry struct {
	// Key is the canonicalized header field key.
	Key string
	// Raw is the raw header field, in the "Key: value" form, including the
	// trailing CRLF. These are exactly the bytes the field was read with:
	// unmodified fields are never re-folded when the header is written.
	// Raw should not be modified and becomes invalid when the header is
	// updated.
	Raw []byte
	// Value is the decoded header field value. If the value cannot be
	// decoded (e.g. it contains a malformed RFC 2047 encoded-word), Value
	// holds the raw field value and Err is non-nil.
	Value string
	// Err is the decoding error for the field value, if any. It only
	// affects the decoded view: Header.Get keeps returning the raw field
	// value regardless.
	Err error
}

// Entries returns all header fields in their original order, as a dual
// raw/decoded view. Fields with the same key are kept in the order they
// appear in the header.
//
// An error is returned only if the raw form of a field cannot be produced
// (e.g. a field added with Add contains invalid characters). Decoding
// failures of individual fields are reported in Entry.Err instead.
func (h *Header) Entries() ([]Entry, error) {
	l := make([]Entry, 0, h.Len())
	fields := h.Fields()
	for fields.Next() {
		raw, err := fields.Raw()
		if err != nil {
			return nil, err
		}
		v, derr := decodeHeaderView(fields.Value())
		l = append(l, Entry{
			Key:   fields.Key(),
			Raw:   raw,
			Value: v,
			Err:   derr,
		})
	}
	return l, nil
}
