package ui

import (
	"bufio"
	"io"
)

// bufioLineReader is a tiny bufio.Reader wrapper exposing a single
// readLine() entrypoint. It exists so [Theme] can keep one buffered
// reader across multiple Ask/Confirm/SelectOne calls — otherwise each
// call would create its own bufio.Reader and steal pre-read bytes
// from the underlying io.Reader.
type bufioLineReader struct {
	br *bufio.Reader
}

func newBufioLineReader(r io.Reader) *bufioLineReader {
	return &bufioLineReader{br: bufio.NewReader(r)}
}

// readLine returns the next line (without trailing newline), or io.EOF
// when no more input is available.
func (b *bufioLineReader) readLine() (string, error) {
	line, err := b.br.ReadString('\n')
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line, err
}
