// Package hostmask matches eggdrop-style nick!user@host masks (* and ?).
package hostmask

import (
	"strings"
	"unicode/utf8"

	"eggbot/internal/irccase"
)

// Normalize builds nick!user@host from parts. Empty user/host become "*".
func Normalize(nick, user, host string) string {
	if user == "" {
		user = "*"
	}
	if host == "" {
		host = "*"
	}
	return nick + "!" + user + "@" + host
}

// Match reports whether nuh (nick!user@host) matches the eggdrop mask.
// Matching is RFC1459-case-insensitive. Masks without '!' are treated as
// nick-only; without '@' as nick!user.
func Match(mask, nuh string) bool {
	mask = strings.TrimSpace(mask)
	if mask == "" {
		return false
	}
	mask = expand(mask)
	return matchFold(irccase.Fold(mask), irccase.Fold(nuh))
}

func expand(mask string) string {
	switch strings.Count(mask, "!") + strings.Count(mask, "@") {
	case 0:
		// "nate" or "*foo*"
		if strings.ContainsAny(mask, "*?") {
			return mask
		}
		return mask + "!*@*"
	}
	if !strings.Contains(mask, "!") && strings.Contains(mask, "@") {
		return "*!" + mask
	}
	if strings.Contains(mask, "!") && !strings.Contains(mask, "@") {
		return mask + "@*"
	}
	return mask
}

func matchFold(pat, s string) bool {
	// iterative glob: * and ?
	type state struct{ pi, si int }
	stack := []state{{0, 0}}
	seen := map[state]bool{}
	for len(stack) > 0 {
		st := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[st] {
			continue
		}
		seen[st] = true
		pi, si := st.pi, st.si
		for pi < len(pat) {
			switch pat[pi] {
			case '*':
				// skip consecutive *
				for pi < len(pat) && pat[pi] == '*' {
					pi++
				}
				if pi == len(pat) {
					return true
				}
				// try remaining pattern at every remaining position
				for sj := si; sj <= len(s); {
					stack = append(stack, state{pi, sj})
					if sj == len(s) {
						break
					}
					_, w := utf8.DecodeRuneInString(s[sj:])
					sj += w
				}
				pi, si = -1, -1
			case '?':
				if si >= len(s) {
					pi, si = -1, -1
					break
				}
				_, w := utf8.DecodeRuneInString(s[si:])
				si += w
				pi++
			default:
				if si >= len(s) || pat[pi] != s[si] {
					pi, si = -1, -1
					break
				}
				pi++
				si++
			}
			if pi < 0 {
				break
			}
		}
		if pi == len(pat) && si == len(s) {
			return true
		}
	}
	return false
}

// Specificity is a rough score for picking the "best" matching mask.
// Longer literal content wins.
func Specificity(mask string) int {
	n := 0
	for i := 0; i < len(mask); i++ {
		switch mask[i] {
		case '*', '?':
		default:
			n++
		}
	}
	return n
}
