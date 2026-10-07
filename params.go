package message

import (
	"strconv"
	"strings"
)

// A ParamEntry records a single occurrence of a MIME header parameter, as
// found while scanning a header field value from left to right.
type ParamEntry struct {
	// Key is the parameter key exactly as written in the header.
	Key string
	// FoldedKey is the case-folded (lowercased) form of Key, used for
	// duplicate detection.
	FoldedKey string
	// Value is the parameter value exactly as written in the header. A
	// quoted-string value keeps its surrounding quotes and escapes.
	Value string
	// Index is the zero-based position of this occurrence in source order.
	Index int
}

// A ParamConflict groups parameter occurrences that resolve to the same
// case-folded logical key. Entries are listed in source order.
type ParamConflict struct {
	// FoldedKey is the case-folded logical key shared by all entries.
	FoldedKey string
	// Entries lists the conflicting occurrences in source order.
	Entries []ParamEntry
}

// ParamDiagnostics is a diagnostic view of the parameters of a MIME header
// field. Unlike ParseMediaType-based helpers, it preserves duplicate
// parameter occurrences instead of rejecting or collapsing them. It is a
// read-only view: the header it was derived from is never modified.
type ParamDiagnostics struct {
	// Entries lists all parameter occurrences in source order.
	Entries []ParamEntry
	// Conflicts lists groups of duplicate occurrences, ordered by the
	// position of their first occurrence. RFC 2231 continuation segments
	// (e.g. filename*0*/filename*1*) of a single parameter are not
	// reported as conflicts.
	Conflicts []ParamConflict
}

// HasConflicts reports whether duplicate parameters were found.
func (d *ParamDiagnostics) HasConflicts() bool {
	return d != nil && len(d.Conflicts) > 0
}

// splitHeaderParams splits a header field value on semicolons, ignoring
// semicolons inside quoted-strings.
func splitHeaderParams(s string) []string {
	var parts []string
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
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// paramKeyKind describes how a parameter key participates in RFC 2231
// encoding.
type paramKeyKind int

const (
	paramKeyPlain        paramKeyKind = iota // e.g. "filename"
	paramKeyExtended                         // e.g. "filename*"
	paramKeyContinuation                     // e.g. "filename*0" or "filename*0*"
)

// analyzeParamKey classifies a case-folded parameter key and returns its
// logical base key. For RFC 2231 continuation segments it also returns the
// segment number.
func analyzeParamKey(foldedKey string) (base string, kind paramKeyKind, segment int) {
	i := strings.LastIndexByte(foldedKey, '*')
	if i < 0 {
		return foldedKey, paramKeyPlain, 0
	}
	base, num := foldedKey[:i], foldedKey[i+1:]
	if strings.HasSuffix(num, "*") {
		num = num[:len(num)-1]
		if n, err := strconv.Atoi(num); err == nil && n >= 0 && base != "" {
			return base, paramKeyContinuation, n
		}
		return foldedKey, paramKeyPlain, 0
	}
	if num == "" {
		// Trailing "*" only: RFC 2231 extended value, e.g. "filename*".
		return base, paramKeyExtended, 0
	}
	if n, err := strconv.Atoi(num); err == nil && n >= 0 && base != "" {
		return base, paramKeyContinuation, n
	}
	return foldedKey, paramKeyPlain, 0
}

// ParseHeaderParamsDiagnostics scans a header field value containing
// parameters (such as a Content-Type or Content-Disposition value) and
// returns the main value together with a diagnostic view of the parameters.
//
// The scan is read-only and lenient: malformed segments are skipped, and
// duplicate parameters are reported in diags instead of causing an error.
// Parameter values are kept exactly as written; no RFC 2047 or RFC 2231
// decoding is applied.
func ParseHeaderParamsDiagnostics(s string) (f string, diags *ParamDiagnostics) {
	parts := splitHeaderParams(s)
	f = strings.TrimSpace(parts[0])
	diags = &ParamDiagnostics{}

	type group struct {
		entries []ParamEntry
		claims  int // number of logical parameters claiming this key
	}
	groups := make(map[string]*group)
	var order []string
	// contSegs tracks continuation segment numbers already seen per logical
	// key, so that the segments of a single RFC 2231 parameter count as one
	// claim while repeated segment numbers count as extra claims.
	contSegs := make(map[string]map[int]bool)

	for _, p := range parts[1:] {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		if key == "" {
			continue
		}
		entry := ParamEntry{
			Key:       key,
			FoldedKey: strings.ToLower(key),
			Value:     value,
			Index:     len(diags.Entries),
		}
		diags.Entries = append(diags.Entries, entry)

		base, kind, segment := analyzeParamKey(entry.FoldedKey)
		g, ok := groups[base]
		if !ok {
			g = &group{}
			groups[base] = g
			order = append(order, base)
		}
		g.entries = append(g.entries, entry)
		switch kind {
		case paramKeyContinuation:
			segs := contSegs[base]
			if segs == nil {
				segs = make(map[int]bool)
				contSegs[base] = segs
			}
			if !segs[segment] {
				segs[segment] = true
				g.claims++ // first segment of a continuation sequence
			} else {
				g.claims++ // repeated segment number: extra claim
			}
		default:
			g.claims++
		}
	}

	for _, base := range order {
		g := groups[base]
		if g.claims > 1 {
			diags.Conflicts = append(diags.Conflicts, ParamConflict{
				FoldedKey: base,
				Entries:   g.entries,
			})
		}
	}
	return f, diags
}

// ContentTypeDiagnostics parses the Content-Type header field like
// ContentType, but returns a diagnostic view of its parameters which
// preserves duplicate occurrences. The header is not modified.
//
// If no Content-Type is specified, it returns "text/plain".
func (h *Header) ContentTypeDiagnostics() (t string, diags *ParamDiagnostics) {
	v := h.Get("Content-Type")
	if v == "" {
		return "text/plain", &ParamDiagnostics{}
	}
	return ParseHeaderParamsDiagnostics(v)
}

// ContentDispositionDiagnostics parses the Content-Disposition header field
// like ContentDisposition, but returns a diagnostic view of its parameters
// which preserves duplicate occurrences. The header is not modified.
func (h *Header) ContentDispositionDiagnostics() (disp string, diags *ParamDiagnostics) {
	return ParseHeaderParamsDiagnostics(h.Get("Content-Disposition"))
}
