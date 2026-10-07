package message

import (
	"strings"
	"testing"
)

// TestContentDispositionDiagnosticsDuplicate verifies that duplicate
// filename parameters are preserved by the diagnostic view, in source
// order, while the original interface keeps its existing behavior (an
// error) and the raw header bytes are left untouched.
func TestContentDispositionDiagnosticsDuplicate(t *testing.T) {
	raw := `attachment; filename="a.txt"; filename="b.txt"`

	var h Header
	h.Set("Content-Disposition", raw)

	// Existing interface: duplicates keep the original policy.
	if _, _, err := h.ContentDisposition(); err == nil {
		t.Fatal("Expected ContentDisposition to keep rejecting duplicates, got nil error")
	}

	disp, diags := h.ContentDispositionDiagnostics()
	if disp != "attachment" {
		t.Errorf("Expected disposition %q but got %q", "attachment", disp)
	}
	if !diags.HasConflicts() {
		t.Fatal("Expected conflicts in diagnostics, got none")
	}
	if len(diags.Entries) != 2 {
		t.Fatalf("Expected 2 entries but got %d", len(diags.Entries))
	}
	if len(diags.Conflicts) != 1 {
		t.Fatalf("Expected 1 conflict but got %d", len(diags.Conflicts))
	}
	c := diags.Conflicts[0]
	if c.FoldedKey != "filename" {
		t.Errorf("Expected conflict folded key %q but got %q", "filename", c.FoldedKey)
	}
	if len(c.Entries) != 2 {
		t.Fatalf("Expected 2 conflicting entries but got %d", len(c.Entries))
	}
	// Source order and original keys/values preserved.
	if c.Entries[0].Key != "filename" || c.Entries[0].Value != `"a.txt"` || c.Entries[0].Index != 0 {
		t.Errorf("Unexpected first entry: %+v", c.Entries[0])
	}
	if c.Entries[1].Key != "filename" || c.Entries[1].Value != `"b.txt"` || c.Entries[1].Index != 1 {
		t.Errorf("Unexpected second entry: %+v", c.Entries[1])
	}

	// The header itself is never rewritten.
	if got := h.Get("Content-Disposition"); got != raw {
		t.Errorf("Header was rewritten: expected %q but got %q", raw, got)
	}
}

// TestContentDispositionDiagnosticsCaseFold verifies that duplicates
// differing only in key case are detected via the folded key, while the
// original keys remain distinguishable in the view.
func TestContentDispositionDiagnosticsCaseFold(t *testing.T) {
	raw := `attachment; Filename="a.txt"; FILENAME="b.txt"`

	var h Header
	h.Set("Content-Disposition", raw)

	_, diags := h.ContentDispositionDiagnostics()
	if len(diags.Conflicts) != 1 {
		t.Fatalf("Expected 1 conflict but got %d", len(diags.Conflicts))
	}
	c := diags.Conflicts[0]
	if c.FoldedKey != "filename" {
		t.Errorf("Expected conflict folded key %q but got %q", "filename", c.FoldedKey)
	}
	if c.Entries[0].Key != "Filename" || c.Entries[0].FoldedKey != "filename" {
		t.Errorf("Unexpected first entry: %+v", c.Entries[0])
	}
	if c.Entries[1].Key != "FILENAME" || c.Entries[1].FoldedKey != "filename" {
		t.Errorf("Unexpected second entry: %+v", c.Entries[1])
	}
	if got := h.Get("Content-Disposition"); got != raw {
		t.Errorf("Header was rewritten: expected %q but got %q", raw, got)
	}
}

// TestContentDispositionDiagnosticsRFC2231Continuation verifies that
// RFC 2231 continuation segments (filename*0*/filename*1*) of a single
// parameter are not mistaken for duplicates.
func TestContentDispositionDiagnosticsRFC2231Continuation(t *testing.T) {
	raw := `attachment; filename*0*=utf-8''hello; filename*1*=%20world.txt`

	var h Header
	h.Set("Content-Disposition", raw)

	// Existing interface: continuations still decode to a single value.
	disp, params, err := h.ContentDisposition()
	if err != nil {
		t.Fatalf("Expected no error for continuation segments, got: %v", err)
	}
	if disp != "attachment" || params["filename"] != "hello world.txt" {
		t.Errorf("Unexpected existing parse: disp=%q params=%v", disp, params)
	}

	_, diags := h.ContentDispositionDiagnostics()
	if diags.HasConflicts() {
		t.Fatalf("Expected no conflicts for continuation segments, got: %+v", diags.Conflicts)
	}
	if len(diags.Entries) != 2 {
		t.Fatalf("Expected 2 entries but got %d", len(diags.Entries))
	}
	if diags.Entries[0].Key != "filename*0*" || diags.Entries[1].Key != "filename*1*" {
		t.Errorf("Unexpected entry keys: %+v", diags.Entries)
	}
	if got := h.Get("Content-Disposition"); got != raw {
		t.Errorf("Header was rewritten: expected %q but got %q", raw, got)
	}
}

// TestContentTypeDiagnostics verifies the diagnostics data flow on the
// Content-Type header field, including the empty-header default.
func TestContentTypeDiagnostics(t *testing.T) {
	var h Header
	if mt, diags := h.ContentTypeDiagnostics(); mt != "text/plain" || diags.HasConflicts() {
		t.Errorf("Unexpected empty Content-Type diagnostics: t=%q diags=%+v", mt, diags)
	}

	raw := `text/plain; Charset="utf-8"; charset="us-ascii"`
	h.Set("Content-Type", raw)
	mt, diags := h.ContentTypeDiagnostics()
	if mt != "text/plain" {
		t.Errorf("Expected media type %q but got %q", "text/plain", mt)
	}
	if len(diags.Conflicts) != 1 || diags.Conflicts[0].FoldedKey != "charset" {
		t.Fatalf("Unexpected conflicts: %+v", diags.Conflicts)
	}
	if got := h.Get("Content-Type"); got != raw {
		t.Errorf("Header was rewritten: expected %q but got %q", raw, got)
	}
}

// TestParseHeaderParamsDiagnosticsQuoted verifies that semicolons inside
// quoted-strings do not split parameters.
func TestParseHeaderParamsDiagnosticsQuoted(t *testing.T) {
	f, diags := ParseHeaderParamsDiagnostics(`attachment; filename="a;b.txt"; size=42`)
	if f != "attachment" {
		t.Errorf("Expected disposition %q but got %q", "attachment", f)
	}
	if diags.HasConflicts() {
		t.Fatalf("Expected no conflicts, got: %+v", diags.Conflicts)
	}
	if len(diags.Entries) != 2 || diags.Entries[0].Value != `"a;b.txt"` {
		t.Errorf("Unexpected entries: %+v", diags.Entries)
	}
}

// TestDiagnosticsEntriesMatchRawHeader verifies that every diagnostic
// entry reflects bytes actually present in the original header value.
func TestDiagnosticsEntriesMatchRawHeader(t *testing.T) {
	raw := `attachment; Filename="a.txt"; FILENAME="b.txt"; filename*0*=utf-8''x; filename*1*=%20y`

	var h Header
	h.Set("Content-Disposition", raw)

	_, diags := h.ContentDispositionDiagnostics()
	for _, e := range diags.Entries {
		if !strings.Contains(raw, e.Key+"="+e.Value) {
			t.Errorf("Entry %+v not found verbatim in raw header %q", e, raw)
		}
	}
}
