// Package irccase implements RFC1459, strict-rfc1459, and ASCII casemapping.
//
// Network identifiers (nicks, channels) use a per-session Mapper from ISUPPORT.
// Application handles use HandleFold, which is stable across networks.
package irccase

import "strings"

type Mapper int

const (
	RFC1459 Mapper = iota
	StrictRFC1459
	ASCII
)

// Fold maps a network identifier with the RFC1459 default. Prefer a
// session Mapper for live IRC identifiers.
func Fold(s string) string { return RFC1459.Fold(s) }

// HandleFold is the stable application-handle mapping. It does not follow
// the connected network's CASEMAPPING token.
func HandleFold(s string) string { return RFC1459.Fold(s) }

func Parse(name string) Mapper {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ascii":
		return ASCII
	case "strict-rfc1459", "strictrfc1459":
		return StrictRFC1459
	default:
		return RFC1459
	}
}

func (m Mapper) Name() string {
	switch m {
	case ASCII:
		return "ascii"
	case StrictRFC1459:
		return "strict-rfc1459"
	default:
		return "rfc1459"
	}
}

func (m Mapper) Fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			b.WriteByte(c + ('a' - 'A'))
		case (m == RFC1459 || m == StrictRFC1459) && c == '[':
			b.WriteByte('{')
		case (m == RFC1459 || m == StrictRFC1459) && c == ']':
			b.WriteByte('}')
		case (m == RFC1459 || m == StrictRFC1459) && c == '\\':
			b.WriteByte('|')
		case m == RFC1459 && c == '~':
			b.WriteByte('^')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func (m Mapper) Equal(a, b string) bool { return m.Fold(a) == m.Fold(b) }

func Equal(a, b string) bool { return Fold(a) == Fold(b) }
