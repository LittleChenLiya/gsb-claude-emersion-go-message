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

// HeaderView is a read-only view of a header field, providing both the raw
// bytes and the decoded value.
type HeaderView struct {
	key   string
	raw   []byte
	value string
	err   error
}

// Key returns the field key.
func (v HeaderView) Key() string { return v.key }

// Raw returns the raw field bytes, including the trailing CRLF.
//
// The returned slice should not be modified.
func (v HeaderView) Raw() []byte { return v.raw }

// Value returns the decoded field value. If the value contains an illegal
// encoded-word, the raw value is returned and the error is non-nil.
func (v HeaderView) Value() (string, error) { return v.value, v.err }

// View returns a read-only view of all header fields, in their original order.
// Each field provides both the raw bytes and the decoded value.
//
// The returned slice should not be modified.
func (h *Header) View() []HeaderView {
	fields := h.Fields()
	var views []HeaderView
	for fields.Next() {
		raw, err := fields.Raw()
		if err != nil {
			// If raw bytes are not available, fall back to the value.
			raw = []byte(fields.Value())
		}
		value, decErr := fields.Text()
		views = append(views, HeaderView{
			key:   fields.Key(),
			raw:   raw,
			value: value,
			err:   decErr,
		})
	}
	return views
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
