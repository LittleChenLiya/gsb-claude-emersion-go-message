package textproto_test

import (
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/emersion/go-message/textproto"
)

func ExampleHeader() {
	var h textproto.Header
	h.Add("From", "<root@nsa.gov>")
	h.Add("To", "<root@gchq.gov.uk>")
	h.Set("Subject", "Tonight's dinner")

	fmt.Println("From: ", h.Get("From"))
	fmt.Println("Has Received: ", h.Has("Received"))

	fmt.Println("Header fields:")
	fields := h.Fields()
	for fields.Next() {
		fmt.Println("  ", fields.Key())
	}
}

func ExampleMultipartReader_LeasePart() {
	// An in-memory multipart stream.
	body := "--boundary\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"The quick brown fox jumps over the lazy dog.\r\n" +
		"--boundary\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"Second part.\r\n" +
		"--boundary--\r\n"

	r := textproto.NewMultipartReader(strings.NewReader(body), "boundary")
	for {
		lease, err := r.LeasePart()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		// Read only the beginning of each part, then abandon the rest.
		// Release drains the remainder so the reader can advance.
		buf := make([]byte, 10)
		n, _ := lease.Read(buf)
		fmt.Printf("part peek: %q (fully consumed: %v)\n", buf[:n], lease.FullyConsumed())
		if err := lease.Release(); err != nil {
			log.Fatal(err)
		}
	}

	// Output:
	// part peek: "The quick " (fully consumed: false)
	// part peek: "Second par" (fully consumed: false)
}
