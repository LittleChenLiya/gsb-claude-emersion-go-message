package message

import (
	"bufio"
	"reflect"
	"strings"
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

const dualViewHeader = "From: sender@example.com\r\n" +
	"Subject: =?utf-8?q?Hello_World?=\r\n" +
	"Subject: =?INVALIDCHARSET?B?dSB1bmRlcnN0YW5k?=\r\n" +
	"To: recipient@example.com\r\n\r\n"

func TestHeaderView(t *testing.T) {
	entity, err := Read(bufio.NewReader(strings.NewReader(dualViewHeader)))
	if err != nil {
		t.Fatalf("Read() returned error: %v", err)
	}
	h := entity.Header

	views := h.View()
	if len(views) != 4 {
		t.Fatalf("View() returned %d fields, want 4", len(views))
	}

	// First field: From, unmodified, verbatim
	if views[0].Key() != "From" {
		t.Errorf("views[0].Key() = %q, want %q", views[0].Key(), "From")
	}
	if got := string(views[0].Raw()); got != "From: sender@example.com\r\n" {
		t.Errorf("views[0].Raw() = %q, want %q", got, "From: sender@example.com\r\n")
	}

	// Second field: Subject, legal encoded-word
	if views[1].Key() != "Subject" {
		t.Errorf("views[1].Key() = %q, want %q", views[1].Key(), "Subject")
	}
	if got := string(views[1].Raw()); got != "Subject: =?utf-8?q?Hello_World?=\r\n" {
		t.Errorf("views[1].Raw() = %q, want %q", got, "Subject: =?utf-8?q?Hello_World?=\r\n")
	}
	if got, err := views[1].Value(); err != nil {
		t.Errorf("views[1].Value() returned error: %v", err)
	} else if got != "Hello World" {
		t.Errorf("views[1].Value() = %q, want %q", got, "Hello World")
	}

	// Third field: Subject, bad encoding (unknown charset)
	if views[2].Key() != "Subject" {
		t.Errorf("views[2].Key() = %q, want %q", views[2].Key(), "Subject")
	}
	if got := string(views[2].Raw()); got != "Subject: =?INVALIDCHARSET?B?dSB1bmRlcnN0YW5k?=\r\n" {
		t.Errorf("views[2].Raw() = %q, want %q", got, "Subject: =?INVALIDCHARSET?B?dSB1bmRlcnN0YW5k?=\r\n")
	}
	if got, err := views[2].Value(); err == nil {
		t.Errorf("views[2].Value() returned no error, want error")
	} else if !IsUnknownCharset(err) {
		t.Errorf("views[2].Value() error = %v, want IsUnknownCharset", err)
	} else if got != "=?INVALIDCHARSET?B?dSB1bmRlcnN0YW5k?=" {
		t.Errorf("views[2].Value() = %q, want raw value", got)
	}

	// Fourth field: To, unmodified, verbatim
	if views[3].Key() != "To" {
		t.Errorf("views[3].Key() = %q, want %q", views[3].Key(), "To")
	}
	if got := string(views[3].Raw()); got != "To: recipient@example.com\r\n" {
		t.Errorf("views[3].Raw() = %q, want %q", got, "To: recipient@example.com\r\n")
	}

	// Header.Get stays compatible: returns the raw (undecoded) value
	if got := h.Get("Subject"); got != "=?utf-8?q?Hello_World?=" {
		t.Errorf("Get(\"Subject\") = %q, want %q", got, "=?utf-8?q?Hello_World?=")
	}
}
