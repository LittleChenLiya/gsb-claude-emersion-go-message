package message

import (
	"reflect"
	"testing"
)

func TestHeader(t *testing.T) {
	mediaType := "text/plain"
	mediaParams := map[string]string{"charset": "utf-8"}
	desc := "Plan de complémentarité de l'Homme"
	disp := "attachment"
	dispParams := map[string]string{"filename": "complémentarité.txt"}

	var h Header
	h.SetContentType(mediaType, mediaParams)
	h.SetText("Content-Description", desc)
	h.SetContentDisposition(disp, dispParams)

	if gotMediaType, gotParams, err := h.ContentType(); err != nil {
		t.Error("Expected no error when parsing content type, but got:", err)
	} else if gotMediaType != mediaType {
		t.Errorf("Expected media type %q but got %q", mediaType, gotMediaType)
	} else if !reflect.DeepEqual(gotParams, mediaParams) {
		t.Errorf("Expected media params %v but got %v", mediaParams, gotParams)
	}

	if gotDesc, err := h.Text("Content-Description"); err != nil {
		t.Error("Expected no error when parsing content description, but got:", err)
	} else if gotDesc != desc {
		t.Errorf("Expected content description %q but got %q", desc, gotDesc)
	}

	if gotDisp, gotParams, err := h.ContentDisposition(); err != nil {
		t.Error("Expected no error when parsing content disposition, but got:", err)
	} else if gotDisp != disp {
		t.Errorf("Expected disposition %q but got %q", disp, gotDisp)
	} else if !reflect.DeepEqual(gotParams, dispParams) {
		t.Errorf("Expected disposition params %v but got %v", dispParams, gotParams)
	}
}

func TestEmptyContentType(t *testing.T) {
	var h Header

	mediaType := "text/plain"
	if gotMediaType, _, err := h.ContentType(); err != nil {
		t.Error("Expected no error when parsing empty content type, but got:", err)
	} else if gotMediaType != mediaType {
		t.Errorf("Expected media type %q but got %q", mediaType, gotMediaType)
	}
}

func TestKnownCharset(t *testing.T) {
	var h Header

	h.Set("Subject", "=?ISO-8859-1?B?SWYgeW91IGNhbiByZWFkIHRoaXMgeW8=?=")

	fields := h.Fields()
	if !fields.Next() {
		t.Error("Expected to be able to advance to first header item")
	}

	_, err := fields.Text()
	if err != nil {
		t.Error("Expected no error when decoding header key of known charset, but got: ", err)
	}
}

func TestUnknownCharset(t *testing.T) {
	var h Header

	h.Set("Subject", "=?INVALIDCHARSET?B?dSB1bmRlcnN0YW5kIHRoZSBleGFtcGxlLg==?=")

	fields := h.Fields()
	if !fields.Next() {
		t.Error("Expected to be able to advance to first header item")
	}

	_, err := fields.Text()
	if err == nil {
		t.Error("Expected error decoding header key of unknown charset")
	}
	if !IsUnknownCharset(err) {
		t.Error("Expected error to verify IsUnknownCharset")
	}
}

func TestContentDispositionDiagnosticsDuplicate(t *testing.T) {
	raw := `attachment; filename="a.txt"; filename="b.txt"`
	var h Header
	h.AddRaw([]byte("Content-Disposition: " + raw))

	disp, params, conflicts, err := h.ContentDispositionDiagnostics()

	// The existing parsing policy is preserved: whatever ContentDisposition
	// returns, ContentDispositionDiagnostics returns too.
	wantDisp, wantParams, wantErr := h.ContentDisposition()
	if disp != wantDisp || err != wantErr {
		t.Errorf("Expected disp/err (%q, %v) but got (%q, %v)", wantDisp, wantErr, disp, err)
	}
	if !reflect.DeepEqual(params, wantParams) {
		t.Errorf("Expected params %v but got %v", wantParams, params)
	}

	// The duplicate is reported in source order, with original and folded keys.
	if len(conflicts) != 1 {
		t.Fatalf("Expected 1 conflict but got %v", conflicts)
	}
	c := conflicts[0]
	if c.Key != "filename" || c.FoldedKey != "filename" {
		t.Errorf("Expected key/folded key filename/filename but got %q/%q", c.Key, c.FoldedKey)
	}
	if c.Value != "b.txt" || c.FirstValue != "a.txt" {
		t.Errorf("Expected values b.txt/a.txt but got %q/%q", c.Value, c.FirstValue)
	}

	// The header is not rewritten by the diagnostic view.
	if got := h.Get("Content-Disposition"); got != raw {
		t.Errorf("Expected header value %q but got %q", raw, got)
	}
	if got, err := h.Raw("Content-Disposition"); err != nil || string(got) != "Content-Disposition: "+raw {
		t.Errorf("Expected raw header %q but got %q (err = %v)", "Content-Disposition: "+raw, got, err)
	}
}

func TestContentDispositionDiagnosticsCaseFolded(t *testing.T) {
	raw := `attachment; FileName="a.txt"; FILENAME="b.txt"`
	var h Header
	h.AddRaw([]byte("Content-Disposition: " + raw))

	_, _, conflicts, _ := h.ContentDispositionDiagnostics()

	if len(conflicts) != 1 {
		t.Fatalf("Expected 1 conflict but got %v", conflicts)
	}
	c := conflicts[0]
	if c.Key != "FILENAME" || c.FoldedKey != "filename" || c.FirstKey != "FileName" {
		t.Errorf("Expected keys FILENAME/filename/FileName but got %q/%q/%q", c.Key, c.FoldedKey, c.FirstKey)
	}
	if c.Value != "b.txt" || c.FirstValue != "a.txt" {
		t.Errorf("Expected values b.txt/a.txt but got %q/%q", c.Value, c.FirstValue)
	}

	// The header is not rewritten by the diagnostic view.
	if got := h.Get("Content-Disposition"); got != raw {
		t.Errorf("Expected header value %q but got %q", raw, got)
	}
}

func TestContentDispositionDiagnosticsRFC2231(t *testing.T) {
	raw := `attachment; filename*0*=utf-8''hello%20; filename*1*=world.txt`
	var h Header
	h.AddRaw([]byte("Content-Disposition: " + raw))

	disp, params, conflicts, err := h.ContentDispositionDiagnostics()
	if err != nil {
		t.Error("Expected no error when parsing RFC 2231 segments, but got:", err)
	}
	if disp != "attachment" {
		t.Errorf("Expected disposition %q but got %q", "attachment", disp)
	}
	if params["filename"] != "hello world.txt" {
		t.Errorf("Expected decoded filename %q but got %q", "hello world.txt", params["filename"])
	}

	// Continuation segments of a single parameter are not duplicates.
	if len(conflicts) != 0 {
		t.Errorf("Expected no conflicts but got %v", conflicts)
	}

	// The header is not rewritten by the diagnostic view.
	if got := h.Get("Content-Disposition"); got != raw {
		t.Errorf("Expected header value %q but got %q", raw, got)
	}
}

func TestContentTypeDiagnostics(t *testing.T) {
	raw := `text/plain; charset=utf-8; CHARSET=us-ascii`
	var h Header
	h.AddRaw([]byte("Content-Type: " + raw))

	mediaType, params, conflicts, err := h.ContentTypeDiagnostics()

	// The existing parsing policy is preserved: whatever ContentType
	// returns, ContentTypeDiagnostics returns too.
	wantMediaType, wantParams, wantErr := h.ContentType()
	if mediaType != wantMediaType || err != wantErr {
		t.Errorf("Expected media type/err (%q, %v) but got (%q, %v)", wantMediaType, wantErr, mediaType, err)
	}
	if !reflect.DeepEqual(params, wantParams) {
		t.Errorf("Expected params %v but got %v", wantParams, params)
	}

	if len(conflicts) != 1 {
		t.Fatalf("Expected 1 conflict but got %v", conflicts)
	}
	c := conflicts[0]
	if c.Key != "CHARSET" || c.FoldedKey != "charset" || c.FirstKey != "charset" {
		t.Errorf("Expected keys CHARSET/charset/charset but got %q/%q/%q", c.Key, c.FoldedKey, c.FirstKey)
	}
	if got := h.Get("Content-Type"); got != raw {
		t.Errorf("Expected header value %q but got %q", raw, got)
	}
}
