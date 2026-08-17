package partyline

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

const (
	iac  = 255
	dont = 254
	do   = 253
	wont = 252
	will = 251
	sb   = 250
	se   = 240
)

var ErrLineTooLong = errors.New("partyline line too long")

// readLine reads one CR/LF-terminated line, stripping telnet IAC negotiation
// so macOS/BSD telnet does not inject WILL/DO bytes into the handle or password.
func readLine(r *bufio.Reader, w io.Writer, max int) (string, error) {
	var b strings.Builder
	for {
		c, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if c == iac {
			if err := skipIAC(r, w); err != nil {
				return "", err
			}
			continue
		}
		if c == '\n' {
			return strings.TrimRight(b.String(), "\r"), nil
		}
		if c == '\r' || c == 0 {
			continue
		}
		if c == 0x08 || c == 0x7f {
			s := b.String()
			if s == "" {
				continue
			}
			b.Reset()
			b.WriteString(s[:len(s)-1])
			continue
		}
		if c >= 32 && c < 127 {
			if max > 0 && b.Len() >= max {
				return "", ErrLineTooLong
			}
			b.WriteByte(c)
		}
	}
}

func skipIAC(r *bufio.Reader, w io.Writer) error {
	cmd, err := r.ReadByte()
	if err != nil {
		return err
	}
	switch cmd {
	case iac: // escaped 0xff
		return nil
	case sb:
		for {
			x, err := r.ReadByte()
			if err != nil {
				return err
			}
			if x != iac {
				continue
			}
			y, err := r.ReadByte()
			if err != nil {
				return err
			}
			if y == se {
				return nil
			}
		}
	case will, wont, do, dont:
		opt, err := r.ReadByte()
		if err != nil {
			return err
		}
		reply := byte(wont)
		if cmd == will || cmd == wont {
			reply = dont
		}
		if w != nil {
			_, _ = w.Write([]byte{iac, reply, opt})
		}
		return nil
	default:
		return nil
	}
}
