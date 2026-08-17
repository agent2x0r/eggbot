package ircx

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ModeKind classifies a mode letter per ISUPPORT CHANMODES + PREFIX.
type ModeKind int

const (
	ModeUnknown   ModeKind = iota
	ModeList               // type A: always takes a parameter
	ModeAlwaysArg          // type B: always takes a parameter
	ModeSetArg             // type C: parameter only when set
	ModeFlag               // type D: never takes a parameter
	ModePrefix             // PREFIX status mode (always takes nick)
)

// ModeChange is one parsed mode letter with optional argument.
type ModeChange struct {
	Add    bool
	Mode   byte
	Arg    string
	Kind   ModeKind
	Prefix byte // '@', '+', etc when Kind is ModePrefix
}

type ModeParser struct {
	prefixModes string
	prefixChars string
	typeA       string
	typeB       string
	typeC       string
	typeD       string
}

func ParserFromISupport(is *ISupport) ModeParser {
	a, b, c, d := is.ChanModes()
	pm, pc := is.Prefix()
	return ModeParser{prefixModes: pm, prefixChars: pc, typeA: a, typeB: b, typeC: c, typeD: d}
}

func (p ModeParser) Kind(mode byte) ModeKind {
	if strings.IndexByte(p.prefixModes, mode) >= 0 {
		return ModePrefix
	}
	if strings.IndexByte(p.typeA, mode) >= 0 {
		return ModeList
	}
	if strings.IndexByte(p.typeB, mode) >= 0 {
		return ModeAlwaysArg
	}
	if strings.IndexByte(p.typeC, mode) >= 0 {
		return ModeSetArg
	}
	if strings.IndexByte(p.typeD, mode) >= 0 {
		return ModeFlag
	}
	return ModeUnknown
}

func (p ModeParser) PrefixChar(mode byte) byte {
	i := strings.IndexByte(p.prefixModes, mode)
	if i >= 0 && i < len(p.prefixChars) {
		return p.prefixChars[i]
	}
	return 0
}

func (p ModeParser) ModeForPrefix(pref byte) byte {
	i := strings.IndexByte(p.prefixChars, pref)
	if i >= 0 && i < len(p.prefixModes) {
		return p.prefixModes[i]
	}
	return 0
}

func (p ModeParser) takesArg(kind ModeKind, add bool) bool {
	switch kind {
	case ModeList, ModeAlwaysArg, ModePrefix:
		return true
	case ModeSetArg:
		return add
	default:
		return false
	}
}

// Parse expands a MODE parameter string and argument list into typed changes.
func (p ModeParser) Parse(modes string, args []string) []ModeChange {
	var out []ModeChange
	add := true
	ai := 0
	for i := 0; i < len(modes); i++ {
		m := modes[i]
		if m == '+' {
			add = true
			continue
		}
		if m == '-' {
			add = false
			continue
		}
		kind := p.Kind(m)
		ch := ModeChange{Add: add, Mode: m, Kind: kind, Prefix: p.PrefixChar(m)}
		if p.takesArg(kind, add) && ai < len(args) {
			ch.Arg = args[ai]
			ai++
		}
		out = append(out, ch)
	}
	return out
}

// SplitStatusMsg strips STATUSMSG prefixes from a target.
func SplitStatusMsg(target, statusmsg, chantypes string) (status, rest string) {
	for len(target) > 0 {
		r, size := utf8.DecodeRuneInString(target)
		if r == utf8.RuneError && size == 1 {
			break
		}
		if strings.ContainsRune(statusmsg, r) {
			status += target[:size]
			target = target[size:]
			continue
		}
		break
	}
	return status, target
}

func IsChannelTarget(target, chantypes string) bool {
	if target == "" {
		return false
	}
	if chantypes == "" {
		chantypes = "#&+!"
	}
	return strings.ContainsRune(chantypes, rune(target[0]))
}

func IsChannelOrStatus(target, chantypes, statusmsg string) bool {
	_, rest := SplitStatusMsg(target, statusmsg, chantypes)
	return IsChannelTarget(rest, chantypes)
}

func StripPrefixes(nick, prefixChars string) (prefixes, rest string) {
	for len(nick) > 0 && strings.ContainsRune(prefixChars, rune(nick[0])) {
		prefixes += nick[:1]
		nick = nick[1:]
	}
	return prefixes, nick
}

func ParseUserhostName(token string) (nick, user, host string) {
	nick = token
	if i := strings.IndexByte(token, '!'); i >= 0 {
		nick = token[:i]
		rest := token[i+1:]
		if j := strings.IndexByte(rest, '@'); j >= 0 {
			user, host = rest[:j], rest[j+1:]
		} else {
			user = rest
		}
	}
	return nick, user, host
}

func ValidNick(nick string, max int) bool {
	if nick == "" || (max > 0 && len(nick) > max) {
		return false
	}
	for i, r := range nick {
		if i == 0 && (r == '#' || r == '&' || r == '+' || r == '!' || r == ':') {
			return false
		}
		if r == ' ' || r == ',' || r == '*' || r == '?' || r == '!' || r == '@' || r == '.' {
			return false
		}
		if r < 33 || r == 127 {
			return false
		}
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}
