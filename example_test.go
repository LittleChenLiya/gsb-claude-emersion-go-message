package message_test

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/emersion/go-message"
)

func ExampleRead() {
	// Let's assume r is an io.Reader that contains a message.
	var r io.Reader

	m, err := message.Read(r)
	if message.IsUnknownCharset(err) {
		// This error is not fatal
		log.Println("Unknown encoding:", err)
	} else if err != nil {
		log.Fatal(err)
	}

	if mr := m.MultipartReader(); mr != nil {
		// This is a multipart message
		log.Println("This is a multipart message containing:")
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			} else if err != nil {
				log.Fatal(err)
			}

			t, _, _ := p.Header.ContentType()
			log.Println("A part with type", t)
		}
	} else {
		t, _, _ := m.Header.ContentType()
		log.Println("This is a non-multipart message with type", t)
	}
}

func ExampleWriter() {
	var b bytes.Buffer

	var h message.Header
	h.SetContentType("multipart/alternative", nil)
	w, err := message.CreateWriter(&b, h)
	if err != nil {
		log.Fatal(err)
	}

	var h1 message.Header
	h1.SetContentType("text/html", nil)
	w1, err := w.CreatePart(h1)
	if err != nil {
		log.Fatal(err)
	}
	io.WriteString(w1, "<h1>Hello World!</h1><p>This is an HTML part.</p>")
	w1.Close()

	var h2 message.Header
	h2.SetContentType("text/plain", nil)
	w2, err := w.CreatePart(h2)
	if err != nil {
		log.Fatal(err)
	}
	io.WriteString(w2, "Hello World!\n\nThis is a text part.")
	w2.Close()

	w.Close()

	log.Println(b.String())
}

func Example_transform() {
	// Let's assume r is an io.Reader that contains a message.
	var r io.Reader

	m, err := message.Read(r)
	if message.IsUnknownCharset(err) {
		log.Println("Unknown encoding:", err)
	} else if err != nil {
		log.Fatal(err)
	}

	// We'll add "This message is powered by Go" at the end of each text entity.
	poweredBy := "\n\nThis message is powered by Go."

	var b bytes.Buffer
	w, err := message.CreateWriter(&b, m.Header)
	if err != nil {
		log.Fatal(err)
	}

	// Define a function that transforms message.
	var transform func(w *message.Writer, e *message.Entity) error
	transform = func(w *message.Writer, e *message.Entity) error {
		if mr := e.MultipartReader(); mr != nil {
			// This is a multipart entity, transform each of its parts
			for {
				p, err := mr.NextPart()
				if err == io.EOF {
					break
				} else if err != nil {
					return err
				}

				pw, err := w.CreatePart(p.Header)
				if err != nil {
					return err
				}

				if err := transform(pw, p); err != nil {
					return err
				}

				pw.Close()
			}
			return nil
		} else {
			body := e.Body
			if strings.HasPrefix(m.Header.Get("Content-Type"), "text/") {
				body = io.MultiReader(body, strings.NewReader(poweredBy))
			}
			_, err := io.Copy(w, body)
			return err
		}
	}

	if err := transform(w, m); err != nil {
		log.Fatal(err)
	}
	w.Close()

	log.Println(b.String())
}

// Regression example for the original interface: Get returns the raw header
// bytes, and ContentDisposition keeps its existing policy of rejecting
// duplicate parameters.
func ExampleHeader_ContentDisposition() {
	var h message.Header
	h.Set("Content-Disposition", `attachment; filename="a.txt"; filename="b.txt"`)

	fmt.Println("raw:", h.Get("Content-Disposition"))
	disp, params, err := h.ContentDisposition()
	fmt.Println("disp:", disp, "params:", params, "err:", err != nil)

	// Output:
	// raw: attachment; filename="a.txt"; filename="b.txt"
	// disp: attachment; filename="a.txt"; filename="b.txt" params: map[] err: true
}

// The diagnostics view preserves duplicate parameter occurrences in source
// order, distinguishing original keys from case-folded keys, without
// rewriting the header. RFC 2231 continuation segments are not reported as
// duplicates.
func ExampleHeader_ContentDispositionDiagnostics() {
	var h message.Header
	h.Set("Content-Disposition", `attachment; Filename="a.txt"; FILENAME="b.txt"; filename*0*=utf-8''hello; filename*1*=%20world.txt`)

	_, diags := h.ContentDispositionDiagnostics()
	for _, c := range diags.Conflicts {
		fmt.Println("conflict:", c.FoldedKey)
		for _, e := range c.Entries {
			fmt.Printf("  #%d key=%q folded=%q value=%s\n", e.Index, e.Key, e.FoldedKey, e.Value)
		}
	}
	fmt.Println("entries:", len(diags.Entries))
	fmt.Println("raw preserved:", h.Get("Content-Disposition"))

	// Output:
	// conflict: filename
	//   #0 key="Filename" folded="filename" value="a.txt"
	//   #1 key="FILENAME" folded="filename" value="b.txt"
	// entries: 4
	// raw preserved: attachment; Filename="a.txt"; FILENAME="b.txt"; filename*0*=utf-8''hello; filename*1*=%20world.txt
}
