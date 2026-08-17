// Package flags implements eggdrop-style user flag sets (global and channel).
package flags

import (
	"fmt"
	"strings"
	"unicode"

	"eggbot/internal/irccase"
)

// Known global/channel flags we honor in v1.
const (
	Owner      = 'n'
	Master     = 'm'
	Op         = 'o'
	Voice      = 'v'
	Friend     = 'f'
	Party      = 'p'
	AutoOp     = 'a'
	AutoVoice  = 'g'
	Deop       = 'd'
	AutoKick   = 'k'
	Quiet      = 'q'
	Botnet     = 't' // reserved
	Xfer       = 'x' // reserved
	Janitor    = 'j' // reserved
	Bot        = 'b' // reserved
	Unshared   = 'u' // reserved
	WasOp      = 'w' // reserved
	WasHalfop  = 'z' // reserved
	NethackEx  = 'e' // reserved
	Common     = 'c' // reserved
	Highlight  = 'h' // reserved
	Halfop     = 'l' // reserved
	AutoHalfop = 'y' // reserved
)

// AllLetters is every letter we accept (a-z and A-Z).
var v1Meaning = map[rune]string{
	Owner:     "owner",
	Master:    "master",
	Op:        "op",
	Voice:     "voice",
	Friend:    "friend",
	Party:     "partyline",
	AutoOp:    "auto-op",
	AutoVoice: "auto-voice",
	Deop:      "deop",
	AutoKick:  "auto-kick",
	Quiet:     "quiet",
}

// Set is an unordered collection of flag letters.
type Set map[rune]struct{}

func Parse(s string) Set {
	out := Set{}
	for _, r := range s {
		if isFlagRune(r) {
			out[r] = struct{}{}
		}
	}
	return out
}

func isFlagRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func (s Set) Has(r rune) bool {
	if s == nil {
		return false
	}
	_, ok := s[r]
	return ok
}

func (s Set) Add(r rune) {
	if s == nil {
		return
	}
	if isFlagRune(r) {
		s[r] = struct{}{}
	}
}

func (s Set) Remove(r rune) { delete(s, r) }

func (s Set) Clone() Set {
	out := Set{}
	for r := range s {
		out[r] = struct{}{}
	}
	return out
}

func (s Set) String() string {
	// stable a-z then A-Z
	var b strings.Builder
	for r := rune('a'); r <= 'z'; r++ {
		if s.Has(r) {
			b.WriteRune(r)
		}
	}
	for r := rune('A'); r <= 'Z'; r++ {
		if s.Has(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s Set) Empty() bool { return len(s) == 0 }

// Apply mutates s according to an eggdrop chattr string like "+nm-o" or "nm".
// A bare letter list (no + or -) replaces the set.
func (s Set) Apply(spec string) error {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return fmt.Errorf("empty flag spec")
	}
	hasSign := strings.ContainsAny(spec, "+-")
	if !hasSign {
		for r := range s {
			delete(s, r)
		}
		for _, r := range spec {
			if r == '|' || r == '&' || unicode.IsSpace(r) {
				continue
			}
			if !isFlagRune(r) {
				return fmt.Errorf("invalid flag %q", string(r))
			}
			s.Add(r)
		}
		return nil
	}
	mode := byte(0)
	for _, r := range spec {
		switch r {
		case '+':
			mode = '+'
		case '-':
			mode = '-'
		case '|', '&':
			// separators between global/channel in classic chattr; ignore here
		default:
			if unicode.IsSpace(r) {
				continue
			}
			if !isFlagRune(r) {
				return fmt.Errorf("invalid flag %q", string(r))
			}
			switch mode {
			case '-':
				s.Remove(r)
			default:
				s.Add(r)
			}
		}
	}
	return nil
}

// MatchAttr evaluates an eggdrop-style flags expression against a user's
// global flags and (optional) channel flags.
//
//	"o"      — has o globally or on the channel
//	"n"      — owner
//	"-"      — anyone (always true)
//	"o|o"    — global o OR channel o (same as "o" in practice)
//	"n|m"    — global n OR channel m  (pipe = OR of the two sides)
//	"nm"     — has n and m (AND of letters on one side)
//
// Classic eggdrop: left of |/& is global, right is channel.
// | is OR, & is AND, between the two sides.
func MatchAttr(global, channel Set, expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" || expr == "-" {
		return true
	}
	sep := byte(0)
	idx := -1
	for i := 0; i < len(expr); i++ {
		if expr[i] == '|' || expr[i] == '&' {
			sep = expr[i]
			idx = i
			break
		}
	}
	if idx < 0 {
		// single side: letters must all be present globally OR all on channel
		return hasAll(global, expr) || hasAll(channel, expr)
	}
	left := expr[:idx]
	right := expr[idx+1:]
	l := hasAll(global, left)
	r := hasAll(channel, right)
	if sep == '&' {
		return l && r
	}
	return l || r
}

func hasAll(s Set, letters string) bool {
	if letters == "" || letters == "-" {
		return true
	}
	for _, r := range letters {
		if !isFlagRune(r) {
			continue
		}
		if !s.Has(r) {
			return false
		}
	}
	return true
}

// Need reports the minimum classic privilege for a command:
// n > m > o > v > p > anyone.
func StrongerOrEqual(have Set, need rune) bool {
	if need == 0 || need == '-' {
		return true
	}
	// owners and masters satisfy lower requirements
	rank := map[rune]int{Owner: 5, Master: 4, Op: 3, Voice: 2, Party: 1}
	needRank := rank[need]
	if have.Has(Owner) && needRank <= 5 {
		return true
	}
	if have.Has(Master) && needRank <= 4 {
		return true
	}
	return have.Has(need)
}

// Combined merges channel flags over global for privilege checks that
// treat "op on this channel" as enough.
func Combined(global, channel Set) Set {
	out := global.Clone()
	for r := range channel {
		out.Add(r)
	}
	return out
}

func FoldHandle(h string) string { return irccase.Fold(h) }

func Meaning(r rune) string {
	if s, ok := v1Meaning[r]; ok {
		return s
	}
	if r >= 'A' && r <= 'Z' {
		return "user-defined"
	}
	return "reserved"
}
