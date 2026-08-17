// Package chanstate persists channel records (chanset, topic, persona, autojoin)
// and the live member list from 353/JOIN/PART.
package chanstate

import (
	"database/sql"
	"strings"
	"sync"
	"time"

	"eggbot/internal/flags"
	"eggbot/internal/irccase"
	"eggbot/internal/store"
)

type Member struct {
	Nick    string
	User    string
	Host    string
	Account string
	Op      bool
	Voice   bool
	Halfop  bool
}

type Channel struct {
	Name       string
	Topic      string
	Modes      string
	Key        string
	Chanset    flags.ChanSet
	Greet      string
	LLMPersona string
	NeedOp     string
	AutoJoin   bool
	Joined     bool               // actually on the channel according to 353/JOIN
	Members    map[string]*Member // folded nick
}

func (c *Channel) Member(nick string) *Member {
	return c.Members[irccase.Fold(nick)]
}

func (c *Channel) Nicks() []string {
	out := make([]string, 0, len(c.Members))
	for _, m := range c.Members {
		out = append(out, m.Nick)
	}
	return out
}

func cloneChannel(c *Channel) *Channel {
	if c == nil {
		return nil
	}
	out := *c
	out.Chanset = c.Chanset.Clone()
	out.Members = make(map[string]*Member, len(c.Members))
	for nick, member := range c.Members {
		if member == nil {
			continue
		}
		copy := *member
		out.Members[nick] = &copy
	}
	return &out
}

type Ban struct {
	Channel   string
	Mask      string
	Reason    string
	Setter    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

func (b Ban) Expired() bool {
	return !b.ExpiresAt.IsZero() && time.Now().After(b.ExpiresAt)
}

type State struct {
	mu    sync.RWMutex
	st    *store.Store
	chans map[string]*Channel
}

func New(st *store.Store) *State {
	return &State{st: st, chans: map[string]*Channel{}}
}

func (s *State) LoadOrCreate(name string, def flags.ChanSet, greet, persona, needOp, key string) *Channel {
	s.mu.Lock()
	defer s.mu.Unlock()
	fold := irccase.Fold(name)
	if c, ok := s.chans[fold]; ok {
		return cloneChannel(c)
	}
	c := &Channel{
		Name:       name,
		Chanset:    def.Clone(),
		Greet:      greet,
		LLMPersona: persona,
		NeedOp:     needOp,
		Key:        key,
		Members:    map[string]*Member{},
	}
	if c.Chanset == nil {
		c.Chanset = flags.ChanSet{}
	}
	var chanset, topic, g, p, n string
	var aj int
	err := s.st.DB.QueryRow(
		`SELECT chanset, topic, greet, llm_persona, need_op, autojoin FROM channels WHERE name = ?`,
		fold,
	).Scan(&chanset, &topic, &g, &p, &n, &aj)
	if err == sql.ErrNoRows {
		_, _ = s.st.DB.Exec(
			`INSERT INTO channels (name, chanset, topic, greet, llm_persona, need_op, autojoin) VALUES (?, ?, '', ?, ?, ?, 0)`,
			fold, c.Chanset.String(), greet, persona, needOp,
		)
	} else if err == nil {
		if parsed, e := flags.ParseChanSet(chanset); e == nil {
			c.Chanset = parsed
		}
		c.Topic = topic
		if g != "" {
			c.Greet = g
		}
		if p != "" {
			c.LLMPersona = p
		}
		if n != "" {
			c.NeedOp = n
		}
		c.AutoJoin = aj != 0
	}
	s.chans[fold] = c
	return cloneChannel(c)
}

func (s *State) Get(name string) *Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneChannel(s.chans[irccase.Fold(name)])
}

func (s *State) All() []*Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Channel, 0, len(s.chans))
	for _, c := range s.chans {
		out = append(out, cloneChannel(c))
	}
	return out
}

func (s *State) Save(c *Channel) error {
	if c == nil {
		return nil
	}
	fold := irccase.Fold(c.Name)
	s.mu.Lock()
	current := s.chans[fold]
	if current == nil {
		current = &Channel{Members: map[string]*Member{}}
		s.chans[fold] = current
	}
	current.Name = c.Name
	current.Topic = c.Topic
	current.Modes = c.Modes
	current.Key = c.Key
	current.Chanset = c.Chanset.Clone()
	current.Greet = c.Greet
	current.LLMPersona = c.LLMPersona
	current.NeedOp = c.NeedOp
	current.AutoJoin = c.AutoJoin
	snapshot := cloneChannel(current)
	s.mu.Unlock()
	return s.persist(snapshot)
}

func (s *State) persist(c *Channel) error {
	fold := irccase.Fold(c.Name)
	aj := 0
	if c.AutoJoin {
		aj = 1
	}
	_, err := s.st.DB.Exec(
		`INSERT INTO channels (name, chanset, topic, greet, llm_persona, need_op, autojoin) VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET chanset=excluded.chanset, topic=excluded.topic, greet=excluded.greet,
		   llm_persona=excluded.llm_persona, need_op=excluded.need_op, autojoin=excluded.autojoin`,
		fold, c.Chanset.String(), c.Topic, c.Greet, c.LLMPersona, c.NeedOp, aj,
	)
	return err
}

func (s *State) SetPersona(name, persona string) {
	s.mu.Lock()
	c := s.chans[irccase.Fold(name)]
	if c == nil {
		s.mu.Unlock()
		return
	}
	c.LLMPersona = persona
	snapshot := cloneChannel(c)
	s.mu.Unlock()
	_ = s.persist(snapshot)
}

func (s *State) SetAutoJoin(name string, on bool) {
	s.mu.Lock()
	c := s.chans[irccase.Fold(name)]
	if c == nil {
		s.mu.Unlock()
		return
	}
	c.AutoJoin = on
	snapshot := cloneChannel(c)
	s.mu.Unlock()
	_ = s.persist(snapshot)
}

func (s *State) SetJoined(name string, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.chans[irccase.Fold(name)]
	if c == nil {
		return
	}
	c.Joined = on
	if !on {
		c.Members = map[string]*Member{}
	}
}

func (s *State) Drop(name string) {
	fold := irccase.Fold(name)
	s.mu.Lock()
	delete(s.chans, fold)
	s.mu.Unlock()
	_, _ = s.st.DB.Exec(`DELETE FROM channels WHERE name = ?`, fold)
}

func (s *State) AutoJoinNames() []string {
	rows, err := s.st.DB.Query(`SELECT name FROM channels WHERE autojoin = 1 ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (s *State) AddMember(channel, nick, user, host, account, prefixes string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.chans[irccase.Fold(channel)]
	if c == nil {
		return
	}
	if c.Members == nil {
		c.Members = make(map[string]*Member)
	}
	m := &Member{Nick: nick, User: user, Host: host, Account: account}
	if strings.Contains(prefixes, "@") || strings.Contains(prefixes, "o") {
		m.Op = true
	}
	if strings.Contains(prefixes, "+") || strings.Contains(prefixes, "v") {
		m.Voice = true
	}
	if strings.Contains(prefixes, "%") || strings.Contains(prefixes, "h") {
		m.Halfop = true
	}
	c.Members[irccase.Fold(nick)] = m
}

func (s *State) RemoveMember(channel, nick string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.chans[irccase.Fold(channel)]
	if c == nil {
		return
	}
	delete(c.Members, irccase.Fold(nick))
}

func (s *State) NickChange(old, neu string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	of, nf := irccase.Fold(old), irccase.Fold(neu)
	for _, c := range s.chans {
		if m, ok := c.Members[of]; ok {
			delete(c.Members, of)
			m.Nick = neu
			c.Members[nf] = m
		}
	}
}

func (s *State) ClearMembers(channel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.chans[irccase.Fold(channel)]
	if c == nil {
		return
	}
	c.Members = map[string]*Member{}
}

func (s *State) SetOp(channel, nick string, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.chans[irccase.Fold(channel)]
	if c == nil {
		return
	}
	if m := c.Members[irccase.Fold(nick)]; m != nil {
		m.Op = on
	}
}

func (s *State) SetVoice(channel, nick string, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.chans[irccase.Fold(channel)]
	if c == nil {
		return
	}
	if m := c.Members[irccase.Fold(nick)]; m != nil {
		m.Voice = on
	}
}

func (s *State) ApplyChanSet(channel, spec string) (*Channel, error) {
	s.mu.Lock()
	c := s.chans[irccase.Fold(channel)]
	if c == nil {
		s.mu.Unlock()
		return nil, sql.ErrNoRows
	}
	if err := c.Chanset.Apply(spec); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	snapshot := cloneChannel(c)
	s.mu.Unlock()
	if err := s.persist(snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (s *State) AddBan(b Ban) error {
	exp := int64(0)
	if !b.ExpiresAt.IsZero() {
		exp = b.ExpiresAt.Unix()
	}
	_, err := s.st.DB.Exec(
		`INSERT INTO bans (channel, mask, reason, setter, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(channel, mask) DO UPDATE SET reason=excluded.reason, setter=excluded.setter, created_at=excluded.created_at, expires_at=excluded.expires_at`,
		irccase.Fold(b.Channel), b.Mask, b.Reason, b.Setter, store.Now(), exp,
	)
	return err
}

func (s *State) DelBan(channel, mask string) error {
	res, err := s.st.DB.Exec(`DELETE FROM bans WHERE channel = ? AND mask = ?`, irccase.Fold(channel), mask)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *State) Bans(channel string) []Ban {
	rows, err := s.st.DB.Query(
		`SELECT channel, mask, reason, setter, created_at, expires_at FROM bans WHERE channel = ?`,
		irccase.Fold(channel),
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Ban
	for rows.Next() {
		var b Ban
		var created, exp int64
		if err := rows.Scan(&b.Channel, &b.Mask, &b.Reason, &b.Setter, &created, &exp); err != nil {
			continue
		}
		b.CreatedAt = time.Unix(created, 0)
		if exp > 0 {
			b.ExpiresAt = time.Unix(exp, 0)
		}
		if !b.Expired() {
			out = append(out, b)
		}
	}
	return out
}

func (s *State) AllBans() []Ban {
	rows, err := s.st.DB.Query(`SELECT channel, mask, reason, setter, created_at, expires_at FROM bans`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Ban
	for rows.Next() {
		var b Ban
		var created, exp int64
		if err := rows.Scan(&b.Channel, &b.Mask, &b.Reason, &b.Setter, &created, &exp); err != nil {
			continue
		}
		b.CreatedAt = time.Unix(created, 0)
		if exp > 0 {
			b.ExpiresAt = time.Unix(exp, 0)
		}
		if !b.Expired() {
			out = append(out, b)
		}
	}
	return out
}
