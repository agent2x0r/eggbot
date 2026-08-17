package flags

import (
	"fmt"
	"sort"
	"strings"
)

// Known channel settings (chansets).
var KnownChanSets = []string{
	"enforcebans",
	"dynamicbans",
	"bitch",
	"protectops",
	"protectfriends",
	"dontkickops",
	"autoop",
	"autovoice",
	"greet",
	"seen",
	"nodesynch",
	"honorackmsg",
	"ai",
	"aitools",
}

var knownSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(KnownChanSets))
	for _, k := range KnownChanSets {
		m[k] = struct{}{}
	}
	return m
}()

// ChanSet is a set of enabled channel settings.
type ChanSet map[string]struct{}

func ParseChanSet(s string) (ChanSet, error) {
	cs := ChanSet{}
	if err := cs.Apply(s); err != nil {
		return nil, err
	}
	return cs, nil
}

func (c ChanSet) Has(name string) bool {
	if c == nil {
		return false
	}
	_, ok := c[strings.ToLower(name)]
	return ok
}

func (c ChanSet) Enable(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if _, ok := knownSet[name]; !ok {
		return fmt.Errorf("unknown chanset %q", name)
	}
	c[name] = struct{}{}
	return nil
}

func (c ChanSet) Disable(name string) {
	delete(c, strings.ToLower(name))
}

// Apply parses "+foo -bar +baz" (or "+foo+bar"). Bare names enable.
func (c ChanSet) Apply(spec string) error {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	// tokenize on spaces, then split glued +foo-bar
	raw := strings.Fields(spec)
	var tokens []string
	for _, r := range raw {
		tokens = append(tokens, splitChanTok(r)...)
	}
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		switch tok[0] {
		case '+':
			if err := c.Enable(tok[1:]); err != nil {
				return err
			}
		case '-':
			name := strings.ToLower(tok[1:])
			if _, ok := knownSet[name]; !ok {
				return fmt.Errorf("unknown chanset %q", name)
			}
			c.Disable(name)
		default:
			if err := c.Enable(tok); err != nil {
				return err
			}
		}
	}
	return nil
}

func splitChanTok(s string) []string {
	var out []string
	start := 0
	for i := 1; i < len(s); i++ {
		if s[i] == '+' || s[i] == '-' {
			out = append(out, s[start:i])
			start = i
		}
	}
	out = append(out, s[start:])
	return out
}

func (c ChanSet) String() string {
	if len(c) == 0 {
		return ""
	}
	names := make([]string, 0, len(c))
	for n := range c {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte('+')
		b.WriteString(n)
	}
	return b.String()
}

func (c ChanSet) Clone() ChanSet {
	out := ChanSet{}
	for n := range c {
		out[n] = struct{}{}
	}
	return out
}

func KnownChanSet(name string) bool {
	_, ok := knownSet[strings.ToLower(name)]
	return ok
}
