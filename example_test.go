package message_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/emersion/go-message"
	"github.com/emersion/go-message/textproto"
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

func ExampleHeader_Entries() {
	r := bufio.NewReader(strings.NewReader("Subject: =?UTF-8?Q?Hello_World?=\r\n" +
		"Subject: Second subject\r\n" +
		"X-Bad-Word: =?UTF-8?Q?bad=ZZ?=\r\n" +
		"X-Folded: a long folded value\r\n" +
		"\twith continuation\r\n" +
		"\r\n"))
	th, err := textproto.ReadHeader(r)
	if err != nil {
		log.Fatal(err)
	}
	h := message.Header{Header: th}

	// Entries exposes both the raw bytes and the decoded value of each
	// field, in their original order.
	entries, err := h.Entries()
	if err != nil {
		log.Fatal(err)
	}
	for _, e := range entries {
		status := "ok"
		if e.Err != nil {
			status = "decode error"
		}
		fmt.Printf("%s: %q (%s)\n", e.Key, e.Value, status)
	}

	// Output:
	// Subject: "Hello World" (ok)
	// Subject: "Second subject" (ok)
	// X-Bad-Word: "=?UTF-8?Q?bad=ZZ?=" (decode error)
	// X-Folded: "a long folded value with continuation" (ok)
}
