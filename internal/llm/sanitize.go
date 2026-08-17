package llm

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// xAI / Grok sometimes dump render directives into the text. We never show those on IRC.
var (
	reGrokRender      = regexp.MustCompile(`(?is)` + "\x3cgrok:render\\b[^>]*\x3e.*?\x3c/grok:render\x3e")
	reGrokRenderEmpty = regexp.MustCompile(`(?i)` + "\x3cgrok:render\\b[^>]*/>")
	reInlineCite      = regexp.MustCompile(`(?i)\s*display\s+render_inline_citation(?:\s+with\s+citation_id\s+is\s+\d+)?`)
	reRenderCite      = regexp.MustCompile(`(?i)\s*render_inline_citation(?:\s+with\s+citation_id(?:\s+is)?\s+\d+)?`)
	reCiteID          = regexp.MustCompile(`(?i)\s*citation_id\s*(?:is|=)\s*\d+`)
	reTurnCite        = regexp.MustCompile(`【[^】]*】`)
	reBracketCite     = regexp.MustCompile(`\[\s*(?:cite|citation|ref|source|footnote)\s*[:#]?\s*[\w.-]+\s*\]`)
	reFootnote        = regexp.MustCompile(`\[\^\w+\]`)
	reCiteLink        = regexp.MustCompile(`\[\[\s*\d+\s*\]\]\((https?://[^)\s]+)\)`)
	reMDLink          = regexp.MustCompile(`\[([^\]]*)\]\((https?://[^)\s]+)\)`)
	reAutoLink        = regexp.MustCompile(`<(https?://[^>\s]+)>`)
	reFence           = regexp.MustCompile("(?s)```[a-zA-Z0-9_-]*\\n?(.*?)```")
	reHeading         = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	reBoldStar        = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reBoldUnder       = regexp.MustCompile(`__([^_]+)__`)
	reInlineCode      = regexp.MustCompile("`([^`]+)`")
	reBullet          = regexp.MustCompile(`(?m)^(?:[-*+]|\d+\.)\s+`)
	reEOS             = regexp.MustCompile(`(?i)\s*(?:<eos>|</s>|<\|endoftext\|>|<\|eot_id\|>)\s*`)
	reLeftoverCite    = regexp.MustCompile(`\[\[\s*\d+\s*\]\]`)
)

func NormalizeIRC(s string) string { return normalizeIRC(s) }

func stripCitations(s string) string {
	s = reGrokRender.ReplaceAllString(s, "")
	s = reGrokRenderEmpty.ReplaceAllString(s, "")
	s = reInlineCite.ReplaceAllString(s, "")
	s = reRenderCite.ReplaceAllString(s, "")
	s = reCiteID.ReplaceAllString(s, "")
	s = reTurnCite.ReplaceAllString(s, "")
	s = reBracketCite.ReplaceAllString(s, "")
	s = reFootnote.ReplaceAllString(s, "")
	return s
}

// normalizeIRC turns model text into a single IRC-safe plain-text blob:
// no markdown, no citation markup, no CTCP/color/control bytes.
func normalizeIRC(s string) string {
	s = stripCitations(s)
	s = reEOS.ReplaceAllString(s, " ")
	s = reCiteLink.ReplaceAllString(s, "$1")
	s = reMDLink.ReplaceAllStringFunc(s, func(m string) string {
		parts := reMDLink.FindStringSubmatch(m)
		if len(parts) != 3 {
			return m
		}
		label, url := strings.TrimSpace(parts[1]), parts[2]
		label = strings.Trim(label, "[]")
		if label == "" || isCiteLabel(label) || strings.EqualFold(label, url) {
			return url
		}
		return label + " — " + url
	})
	s = reAutoLink.ReplaceAllString(s, "$1")
	s = reFence.ReplaceAllString(s, "$1")
	s = reHeading.ReplaceAllString(s, "")
	s = reBoldStar.ReplaceAllString(s, "$1")
	s = reBoldUnder.ReplaceAllString(s, "$1")
	s = reInlineCode.ReplaceAllString(s, "$1")
	s = reBullet.ReplaceAllString(s, "")
	s = reLeftoverCite.ReplaceAllString(s, "")
	s = stripControls(s)
	return tidyLines(s)
}

func isCiteLabel(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func stripControls(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case 0x01, 0x02, 0x0f, 0x16, 0x1d, 0x1e, 0x1f:
			continue
		case 0x03:
			i = skipColor(s, i+1) - 1
			continue
		case 0x04:
			i = skipHexColor(s, i+1) - 1
			continue
		case '\t':
			b.WriteByte(' ')
		default:
			if c < 0x20 || c == 0x7f {
				continue
			}
			b.WriteByte(c)
		}
	}
	return b.String()
}

func skipColor(s string, i int) int {
	n := 0
	for n < 2 && i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		n++
	}
	if i < len(s) && s[i] == ',' {
		i++
		n = 0
		for n < 2 && i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			n++
		}
	}
	return i
}

func skipHexColor(s string, i int) int {
	n := 0
	for n < 8 && i < len(s) {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') || c == ',' {
			i++
			n++
			continue
		}
		break
	}
	return i
}

func tidyLines(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func boundString(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if cut == 0 {
		return ""
	}
	return strings.TrimSpace(s[:cut]) + "…"
}
