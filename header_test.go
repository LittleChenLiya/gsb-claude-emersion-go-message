package message

import (
	"bufio"
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/emersion/go-message/textproto"
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

// Four header fields: two Subject fields (the first one holding a valid
// encoded-word), one field with a malformed encoded-word, and one folded
// field.
const entriesTestHeader = "Subject: =?UTF-8?Q?Hello_World?=\r\n" +
	"Subject: Second subject\r\n" +
	"X-Bad-Word: =?UTF-8?Q?bad=ZZ?=\r\n" +
	"X-Folded: a long folded value\r\n" +
	"\twith continuation\r\n" +
	"\r\n"

func TestHeaderEntries(t *testing.T) {
	th, err := textproto.ReadHeader(bufio.NewReader(strings.NewReader(entriesTestHeader)))
	if err != nil {
		t.Fatal("Expected no error when reading header, but got:", err)
	}
	h := Header{th}

	entries, err := h.Entries()
	if err != nil {
		t.Fatal("Expected no error when listing entries, but got:", err)
	}
	if len(entries) != 4 {
		t.Fatalf("Expected 4 entries, but got %v", len(entries))
	}

	// Duplicate fields are kept in their original order.
	if entries[0].Key != "Subject" || entries[1].Key != "Subject" {
		t.Errorf("Expected first two entries to be Subject, but got %q and %q",
			entries[0].Key, entries[1].Key)
	}

	// Valid encoded-words are decoded in the decoded view.
	if entries[0].Err != nil {
		t.Error("Expected no error when decoding first Subject, but got:", entries[0].Err)
	}
	if want := "Hello World"; entries[0].Value != want {
		t.Errorf("Expected first Subject to decode to %q, but got %q", want, entries[0].Value)
	}
	if entries[1].Err != nil {
		t.Error("Expected no error when decoding second Subject, but got:", entries[1].Err)
	}
	if want := "Second subject"; entries[1].Value != want {
		t.Errorf("Expected second Subject to be %q, but got %q", want, entries[1].Value)
	}

	// A malformed encoded-word is only reported in the decoded view: the raw
	// bytes are preserved and Header.Get is unaffected.
	bad := entries[2]
	if bad.Key != "X-Bad-Word" {
		t.Errorf("Expected third entry to be X-Bad-Word, but got %q", bad.Key)
	}
	if bad.Err == nil {
		t.Error("Expected an error when decoding malformed encoded-word")
	}
	const badRaw = "X-Bad-Word: =?UTF-8?Q?bad=ZZ?=\r\n"
	if string(bad.Raw) != badRaw {
		t.Errorf("Expected raw bytes %q, but got %q", badRaw, string(bad.Raw))
	}
	const badValue = "=?UTF-8?Q?bad=ZZ?="
	if bad.Value != badValue {
		t.Errorf("Expected raw value %q on decode error, but got %q", badValue, bad.Value)
	}
	if got := h.Get("X-Bad-Word"); got != badValue {
		t.Errorf("Expected Get to return raw value %q, but got %q", badValue, got)
	}

	// The folded field is unfolded in the decoded view, but its raw bytes
	// keep the original folding.
	folded := entries[3]
	if folded.Err != nil {
		t.Error("Expected no error when decoding folded field, but got:", folded.Err)
	}
	if want := "a long folded value with continuation"; folded.Value != want {
		t.Errorf("Expected folded value %q, but got %q", want, folded.Value)
	}
	if !bytes.Contains(folded.Raw, []byte("\r\n\t")) {
		t.Errorf("Expected raw bytes to keep the folding, but got %q", string(folded.Raw))
	}

	// Unmodified fields are written back byte-for-byte, without re-folding.
	var b bytes.Buffer
	if err := textproto.WriteHeader(&b, h.Header); err != nil {
		t.Fatal("Expected no error when writing header, but got:", err)
	}
	if b.String() != entriesTestHeader {
		t.Errorf("Expected written header to match the original bytes.\nGot:  %q\nWant: %q",
			b.String(), entriesTestHeader)
	}
}
