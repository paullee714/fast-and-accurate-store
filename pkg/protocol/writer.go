package protocol

import (
	"bytes"
	"fmt"
	"io"
)

// Writer helps in writing data in RESP-like format.
type Writer struct {
	writer io.Writer
}

// NewWriter creates a new Writer.
func NewWriter(w io.Writer) *Writer {
	return &Writer{writer: w}
}

// WriteCommand writes a command as a RESP Array of Bulk Strings.
// Format: *<argc>\r\n$<len>\r\n<arg>\r\n...
func (w *Writer) WriteCommand(args []string) error {
	buf := new(bytes.Buffer)

	// Write array header
	fmt.Fprintf(buf, "*%d\r\n", len(args))

	// Write each argument as bulk string
	for _, arg := range args {
		fmt.Fprintf(buf, "$%d\r\n%s\r\n", len(arg), arg)
	}

	_, err := w.writer.Write(buf.Bytes())
	return err
}

// WriteString writes a simple string response.
// Format: +<string>\r\n
func (w *Writer) WriteString(s string) error {
	_, err := fmt.Fprintf(w.writer, "+%s\r\n", s)
	return err
}

// WriteError writes an error response.
// Format: -<error>\r\n
func (w *Writer) WriteError(err error) error {
	_, e := fmt.Fprintf(w.writer, "-%s\r\n", err.Error())
	return e
}

// WriteBulkString writes a bulk string response.
// Format: $<len>\r\n<data>\r\n
func (w *Writer) WriteBulkString(s string) error {
	_, err := fmt.Fprintf(w.writer, "$%d\r\n%s\r\n", len(s), s)
	return err
}

// WriteNull writes a null response (for nil values).
// Format: $-1\r\n
func (w *Writer) WriteNull() error {
	_, err := fmt.Fprintf(w.writer, "$-1\r\n")
	return err
}

// WriteInteger writes an integer response.
// Format: :<integer>\r\n
func (w *Writer) WriteInteger(n int) error {
	_, err := fmt.Fprintf(w.writer, ":%d\r\n", n)
	return err
}
