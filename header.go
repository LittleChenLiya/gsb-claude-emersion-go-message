package message

import (
	"mime"
	"strconv"
	"strings"

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

// A ParamConflict describes a duplicate parameter found while parsing a
// header field value with parameters, such as Content-Type or
// Content-Disposition.
type ParamConflict struct {
	// Key is the parameter key of the duplicate occurrence, as written in
	// the header (original case).
	Key string
	// FoldedKey is the case-folded parameter key used to detect the
	// conflict. RFC 2231 extended and continuation forms (e.g. "filename*"
	// or "filename*0*") are folded to their base key (e.g. "filename").
	FoldedKey string
	// Value is the parameter value of the duplicate occurrence.
	Value string
	// FirstKey is the parameter key of the first occurrence, as written in
	// the header (original case).
	FirstKey string
	// FirstValue is the parameter value of the first occurrence.
	FirstValue string
}

// splitParamFields splits a header field value at ';' boundaries that are not
// inside a quoted-string. The first element is the main value (e.g. the media
// type), the remaining elements are raw "key=value" parameter fields.
func splitParamFields(s string) []string {
	var fields []string
	start := 0
	inQuote := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case inQuote && c == '\\':
			escaped = true
		case c == '"':
			inQuote = !inQuote
		case c == ';' && !inQuote:
			fields = append(fields, s[start:i])
			start = i + 1
		}
	}
	return append(fields, s[start:])
}

// parseParamKey folds a parameter key for duplicate detection: it is
// lower-cased and RFC 2231 extended/continuation markers ("*", "*N", "*N*")
// are stripped, so that continuation segments of a single parameter (e.g.
// "filename*0*" and "filename*1*") are not mistaken for duplicates. The
// returned segment is the continuation segment number, or an empty string
// for plain and extended keys.
func parseParamKey(key string) (base, segment string) {
	lower := strings.ToLower(key)
	i := strings.IndexByte(lower, '*')
	if i < 0 {
		return lower, ""
	}
	suffix := lower[i+1:]
	if suffix == "" {
		return lower[:i], "" // extended value: "name*"
	}
	num := strings.TrimSuffix(suffix, "*")
	if _, err := strconv.Atoi(num); err == nil {
		return lower[:i], num // continuation segment: "name*N" or "name*N*"
	}
	return lower, ""
}

// unquoteParamValue strips the quotes of a quoted-string parameter value and
// resolves backslash escapes. Values that are not quoted are returned as-is.
func unquoteParamValue(v string) string {
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return v
	}
	v = v[1 : len(v)-1]
	if strings.IndexByte(v, '\\') < 0 {
		return v
	}
	var sb strings.Builder
	escaped := false
	for _, ch := range v {
		if escaped {
			escaped = false
		} else if ch == '\\' {
			escaped = true
			continue
		}
		sb.WriteRune(ch)
	}
	return sb.String()
}

// scanParamConflicts scans a header field value with parameters and reports
// duplicate parameters in source order. It is a read-only diagnostic view:
// the header is never modified. RFC 2231 continuation segments of a single
// parameter are not reported as duplicates.
func scanParamConflicts(s string) []ParamConflict {
	fields := splitParamFields(s)
	if len(fields) < 2 {
		return nil
	}
	type occurrence struct {
		key, value string
	}
	first := make(map[string]occurrence)             // base key -> first plain/extended occurrence
	firstSegment := make(map[string]occurrence)      // base key -> first continuation segment
	segments := make(map[string]map[string]struct{}) // base key -> seen segment numbers
	var conflicts []ParamConflict
	for _, f := range fields[1:] {
		eq := strings.IndexByte(f, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(f[:eq])
		if key == "" {
			continue
		}
		value := unquoteParamValue(strings.TrimSpace(f[eq+1:]))
		base, segment := parseParamKey(key)
		if segment != "" {
			// RFC 2231 continuation segment: distinct segment numbers of
			// the same parameter are not duplicates.
			segs := segments[base]
			if segs == nil {
				segs = make(map[string]struct{})
				segments[base] = segs
			}
			if _, ok := segs[segment]; ok {
				conflicts = append(conflicts, ParamConflict{
					Key:        key,
					FoldedKey:  base,
					Value:      value,
					FirstKey:   firstSegment[base].key,
					FirstValue: firstSegment[base].value,
				})
			} else if prev, ok := first[base]; ok {
				conflicts = append(conflicts, ParamConflict{
					Key:        key,
					FoldedKey:  base,
					Value:      value,
					FirstKey:   prev.key,
					FirstValue: prev.value,
				})
			} else {
				segs[segment] = struct{}{}
				if _, ok := firstSegment[base]; !ok {
					firstSegment[base] = occurrence{key: key, value: value}
				}
			}
			continue
		}
		prev, ok := first[base]
		if !ok {
			prev, ok = firstSegment[base]
		}
		if ok {
			conflicts = append(conflicts, ParamConflict{
				Key:        key,
				FoldedKey:  base,
				Value:      value,
				FirstKey:   prev.key,
				FirstValue: prev.value,
			})
		} else {
			first[base] = occurrence{key: key, value: value}
		}
	}
	return conflicts
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

// ContentTypeDiagnostics parses the Content-Type header field like
// ContentType, and additionally reports duplicate parameters.
//
// The returned media type, params and error are exactly what ContentType
// returns; the header is never modified. conflicts lists duplicate
// parameters in source order, distinguishing keys as written in the header
// from their case-folded form. RFC 2231 continuation segments (e.g.
// "filename*0*" and "filename*1*") are treated as a single parameter and are
// not reported as duplicates.
func (h *Header) ContentTypeDiagnostics() (t string, params map[string]string, conflicts []ParamConflict, err error) {
	v := h.Get("Content-Type")
	if v == "" {
		return "text/plain", nil, nil, nil
	}
	t, params, err = parseHeaderWithParams(v)
	return t, params, scanParamConflicts(v), err
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

// ContentDispositionDiagnostics parses the Content-Disposition header field
// like ContentDisposition, and additionally reports duplicate parameters. See
// ContentTypeDiagnostics for details about the returned values.
func (h *Header) ContentDispositionDiagnostics() (disp string, params map[string]string, conflicts []ParamConflict, err error) {
	v := h.Get("Content-Disposition")
	disp, params, err = parseHeaderWithParams(v)
	return disp, params, scanParamConflicts(v), err
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
