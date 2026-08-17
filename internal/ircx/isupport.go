package ircx

import (
	"strconv"
	"strings"
	"sync"

	"eggbot/internal/irccase"
)

// ISupport is the live RPL_ISUPPORT (005) token map for one IRC session.
type ISupport struct {
	mu     sync.RWMutex
	tokens map[string]string
}

func NewISupport() *ISupport {
	s := &ISupport{tokens: map[string]string{}}
	s.resetDefaults()
	return s
}

func (s *ISupport) resetDefaults() {
	s.tokens["CASEMAPPING"] = "rfc1459"
	s.tokens["CHANTYPES"] = "#&+!"
	s.tokens["PREFIX"] = "(ov)@+"
	s.tokens["CHANMODES"] = "b,k,l,imnt"
	s.tokens["MODES"] = "4"
	s.tokens["NICKLEN"] = "16"
	s.tokens["CHANNELLEN"] = "50"
	s.tokens["TOPICLEN"] = "390"
	s.tokens["KICKLEN"] = "390"
	s.tokens["LINELEN"] = "512"
}

// ApplyTokens parses one 005 parameter list (excluding the trailing text).
// Tokens prefixed with '-' are removed. Values may be empty.
func (s *ISupport) ApplyTokens(params []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, tok := range params {
		if tok == "" || strings.ContainsRune(tok, ' ') {
			continue
		}
		if strings.HasPrefix(tok, "-") {
			delete(s.tokens, strings.ToUpper(strings.TrimPrefix(tok, "-")))
			continue
		}
		name, val, _ := strings.Cut(tok, "=")
		s.tokens[strings.ToUpper(name)] = unescapeISupport(val)
	}
}

func unescapeISupport(v string) string {
	v = strings.ReplaceAll(v, "\\x20", " ")
	v = strings.ReplaceAll(v, "\\x5C", "\\")
	v = strings.ReplaceAll(v, "\\x3D", "=")
	return v
}

func (s *ISupport) Get(name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.tokens[strings.ToUpper(name)]
	return v, ok
}

func (s *ISupport) GetDefault(name, fallback string) string {
	if v, ok := s.Get(name); ok && v != "" {
		return v
	}
	return fallback
}

func (s *ISupport) Has(name string) bool {
	_, ok := s.Get(name)
	return ok
}

func (s *ISupport) Int(name string, fallback int) int {
	v, ok := s.Get(name)
	if !ok || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func (s *ISupport) Mapper() irccase.Mapper {
	return irccase.Parse(s.GetDefault("CASEMAPPING", "rfc1459"))
}

func (s *ISupport) ChanTypes() string {
	return s.GetDefault("CHANTYPES", "#&+!")
}

func (s *ISupport) StatusMsg() string {
	return s.GetDefault("STATUSMSG", "")
}

func (s *ISupport) LineLen() int {
	n := s.Int("LINELEN", 512)
	if n < 64 {
		return 64
	}
	return n
}

func (s *ISupport) NickLen() int { return s.Int("NICKLEN", 16) }

func (s *ISupport) Prefix() (modes, prefixes string) {
	raw := s.GetDefault("PREFIX", "(ov)@+")
	if !strings.HasPrefix(raw, "(") {
		return "ov", "@+"
	}
	end := strings.IndexByte(raw, ')')
	if end < 2 || end+1 >= len(raw) {
		return "ov", "@+"
	}
	modes = raw[1:end]
	prefixes = raw[end+1:]
	if len(modes) != len(prefixes) {
		return "ov", "@+"
	}
	return modes, prefixes
}

func (s *ISupport) ChanModes() (a, b, c, d string) {
	raw := s.GetDefault("CHANMODES", "b,k,l,imnt")
	parts := strings.Split(raw, ",")
	for len(parts) < 4 {
		parts = append(parts, "")
	}
	return parts[0], parts[1], parts[2], parts[3]
}

func (s *ISupport) Snapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.tokens))
	for k, v := range s.tokens {
		out[k] = v
	}
	return out
}

func (s *ISupport) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = map[string]string{}
	s.resetDefaults()
}
