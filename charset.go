package message

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
)

type UnknownCharsetError struct {
	e error
}

func (u UnknownCharsetError) Unwrap() error { return u.e }

func (u UnknownCharsetError) Error() string {
	return "unknown charset: " + u.e.Error()
}

// IsUnknownCharset returns a boolean indicating whether the error is known to
// report that the charset advertised by the entity is unknown.
func IsUnknownCharset(err error) bool {
	return errors.As(err, new(UnknownCharsetError))
}

// CharsetReader, if non-nil, defines a function to generate charset-conversion
// readers, converting from the provided charset into UTF-8. Charsets are always
// lower-case. utf-8 and us-ascii charsets are handled by default. One of the
// the CharsetReader's result values must be non-nil.
//
// Importing github.com/emersion/go-message/charset will set CharsetReader to
// a function that handles most common charsets. Alternatively, CharsetReader
// can be set to e.g. golang.org/x/net/html/charset.NewReaderLabel.
var CharsetReader func(charset string, input io.Reader) (io.Reader, error)

// charsetReader calls CharsetReader if non-nil.
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	charset = strings.ToLower(charset)
	if charset == "utf-8" || charset == "us-ascii" {
		return input, nil
	}
	if CharsetReader != nil {
		r, err := CharsetReader(charset, input)
		if err != nil {
			return r, UnknownCharsetError{err}
		}
		return r, nil
	}
	return input, UnknownCharsetError{fmt.Errorf("message: unhandled charset %q", charset)}
}

// decodeHeader decodes an internationalized header field. If it fails, it
// returns the input string and the error.
func decodeHeader(s string) (string, error) {
	wordDecoder := mime.WordDecoder{CharsetReader: charsetReader}
	dec, err := wordDecoder.DecodeHeader(s)
	if err != nil {
		return s, err
	}
	return dec, nil
}

// hasEncodedWordShape reports whether word has the framing of an RFC 2047
// encoded-word: "=?<charset>?<encoding>?<text>?=".
func hasEncodedWordShape(word string) bool {
	if len(word) < 8 || !strings.HasPrefix(word, "=?") ||
		!strings.HasSuffix(word, "?=") || strings.Count(word, "?") != 4 {
		return false
	}
	inner := word[2 : len(word)-2]
	i := strings.IndexByte(inner, '?')
	if i <= 0 { // the charset cannot be empty
		return false
	}
	// The encoding is a single character, the encoded text may be empty.
	return strings.IndexByte(inner[i+1:], '?') == 1
}

// checkEncodedWords scans s for RFC 2047 encoded-word candidates. It returns
// an error if a candidate has the encoded-word framing but fails to decode.
// Tokens without the encoded-word framing are literal text, as per RFC 2047.
func checkEncodedWords(s string) error {
	wordDecoder := mime.WordDecoder{CharsetReader: charsetReader}
	for {
		i := strings.Index(s, "=?")
		if i < 0 {
			return nil
		}
		s = s[i+2:]
		j := strings.Index(s, "?=")
		if j < 0 {
			return nil
		}
		if word := "=?" + s[:j+2]; hasEncodedWordShape(word) {
			if _, err := wordDecoder.Decode(word); err != nil {
				return fmt.Errorf("message: malformed encoded-word %q: %w", word, err)
			}
		}
	}
}

// decodeHeaderView decodes an internationalized header field for the decoded
// view of a header entry. Unlike decodeHeader, malformed encoded-words are
// reported as an error. If decoding fails, it returns the input string and
// the error.
func decodeHeaderView(s string) (string, error) {
	if err := checkEncodedWords(s); err != nil {
		return s, err
	}
	return decodeHeader(s)
}

func encodeHeader(s string) string {
	return mime.QEncoding.Encode("utf-8", s)
}
