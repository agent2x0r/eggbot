package ircx

import (
	"sort"
	"strings"
	"sync"
)

// DefaultPreferredCaps is the production IRCv3 request set. Capabilities are
// requested only when the server advertises them. echo-message is omitted
// until self-message deduplication is complete.
var DefaultPreferredCaps = []string{
	"account-notify",
	"account-tag",
	"away-notify",
	"batch",
	"cap-notify",
	"chghost",
	"extended-join",
	"invite-notify",
	"message-tags",
	"multi-prefix",
	"sasl",
	"server-time",
	"setname",
	"userhost-in-names",
}

// DisabledCaps are never requested, even if advertised.
var DisabledCaps = map[string]struct{}{
	"echo-message":      {},
	"draft/multiline":   {},
	"draft/chathistory": {},
	"draft/read-marker": {},
	"draft/react":       {},
	"draft/typing":      {},
}

type capValue struct {
	value string
	ack   bool
}

// Caps is the negotiated capability set for one session.
type Caps struct {
	mu      sync.RWMutex
	offered map[string]string
	enabled map[string]capValue
	lsDone  bool
	version int
}

func NewCaps() *Caps {
	return &Caps{offered: map[string]string{}, enabled: map[string]capValue{}, version: 302}
}

func (c *Caps) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offered = map[string]string{}
	c.enabled = map[string]capValue{}
	c.lsDone = false
	c.version = 302
}

func parseCapList(list string) map[string]string {
	out := map[string]string{}
	for _, tok := range strings.Fields(list) {
		name, val, _ := strings.Cut(tok, "=")
		name = strings.ToLower(name)
		if name == "" {
			continue
		}
		out[name] = val
	}
	return out
}

func (c *Caps) AddLS(list string, more bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for name, val := range parseCapList(list) {
		c.offered[name] = val
	}
	if !more {
		c.lsDone = true
	}
}

func (c *Caps) Offered(name string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.offered[strings.ToLower(name)]
	return v, ok
}

func (c *Caps) Enabled(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.enabled[strings.ToLower(name)]
	return ok
}

func (c *Caps) ACK(list string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for name, val := range parseCapList(list) {
		c.enabled[name] = capValue{value: val, ack: true}
	}
}

func (c *Caps) NAK(list string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for name := range parseCapList(list) {
		delete(c.enabled, name)
	}
}

func (c *Caps) DEL(list string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var lost []string
	for name := range parseCapList(list) {
		if _, ok := c.enabled[name]; ok {
			lost = append(lost, name)
		}
		delete(c.enabled, name)
		delete(c.offered, name)
	}
	return lost
}

func (c *Caps) NEW(list string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for name, val := range parseCapList(list) {
		c.offered[name] = val
	}
}

func (c *Caps) LSDone() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lsDone
}

func (c *Caps) MarkLSDone() {
	c.mu.Lock()
	c.lsDone = true
	c.mu.Unlock()
}

func (c *Caps) OfferedNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.offered))
	for n := range c.offered {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (c *Caps) EnabledNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.enabled))
	for n := range c.enabled {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (c *Caps) SASLMechs() []string {
	v, ok := c.Offered("sasl")
	if !ok {
		return nil
	}
	if v == "" {
		return []string{"PLAIN", "EXTERNAL"}
	}
	return strings.Split(v, ",")
}

// SelectRequests returns advertised ∩ preferred, minus disabled capabilities.
// sasl is included only when wantSASL is true.
func SelectRequests(offered map[string]string, preferred []string, wantSASL bool) []string {
	if len(preferred) == 0 {
		preferred = DefaultPreferredCaps
	}
	var out []string
	seen := map[string]struct{}{}
	for _, name := range preferred {
		name = strings.ToLower(name)
		if _, bad := DisabledCaps[name]; bad {
			continue
		}
		if name == "sasl" && !wantSASL {
			continue
		}
		if _, ok := offered[name]; !ok {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func (c *Caps) OfferedMap() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]string, len(c.offered))
	for k, v := range c.offered {
		out[k] = v
	}
	return out
}
