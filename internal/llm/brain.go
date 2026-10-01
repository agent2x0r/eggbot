// Package llm is the bot brain: OpenAI-compatible chat, optional xAI web/X search,
// a SEARCH-vs-LOCAL router, a per-nick FIFO ask queue, and in-memory channel
// scrollback (refilled from a SQLite snapshot after restarts shorter than
// ChatHistMaxAge). Citations from the Responses API are stripped
// before anything is sent to IRC.
package llm

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"eggbot/internal/config"
	"eggbot/internal/flags"
	"eggbot/internal/irccase"
	"eggbot/internal/userfile"
)

type HistoryLine struct {
	Nick string
	Text string
	At   time.Time
}

type Actor interface {
	MatchAttr(handle, expr, channel string) bool
	RunTool(name string, ctx ToolCtx, args map[string]any) (string, error)
}

type Brain struct {
	Cfg    *config.Config
	Client *Client
	Log    *slog.Logger
	Actor  Actor
	Tools  []ToolDef

	mu        sync.Mutex
	hist      map[string][]HistoryLine
	userN     map[string][]time.Time
	chanN     map[string][]time.Time
	inflight  map[string]bool
	queue     map[string][]askJob
	searchAck map[string]bool // one "Searching..." per busy session
	gen       map[string]uint64
	jobs      int
	closed    bool
	cancel    context.CancelFunc
}

const (
	maxAskQueue = 8
	maxAskJobs  = 32
	maxRateKeys = 4096
)

type askJob struct {
	req  AskReq
	done chan askResult
}

type askResult struct {
	text string
	err  error
}

func NewBrain(cfg *config.Config, log *slog.Logger, actor Actor, tools []ToolDef) *Brain {
	ctx, cancel := context.WithCancel(context.Background())
	c := NewClient(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model, cfg.LLMTimeout())
	c.Ctx = ctx
	c.StoreProvider = cfg.LLM.StoreProvider
	c.SearchModel = cfg.LLM.SearchModel
	c.SearchReasoning = cfg.LLM.SearchReasoning
	return &Brain{
		Cfg: cfg, Client: c, Log: log, Actor: actor, Tools: tools,
		hist:      map[string][]HistoryLine{},
		userN:     map[string][]time.Time{},
		chanN:     map[string][]time.Time{},
		inflight:  map[string]bool{},
		queue:     map[string][]askJob{},
		searchAck: map[string]bool{},
		gen:       map[string]uint64{},
		cancel:    cancel,
	}
}

func (b *Brain) ReplaceConfig(cfg *config.Config) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Cfg = cfg
	if b.Client != nil {
		b.Client.BaseURL = strings.TrimRight(cfg.LLM.BaseURL, "/")
		b.Client.APIKey = cfg.LLM.APIKey
		b.Client.Model = cfg.LLM.Model
		b.Client.SearchModel = cfg.LLM.SearchModel
		b.Client.SearchReasoning = cfg.LLM.SearchReasoning
		b.Client.StoreProvider = cfg.LLM.StoreProvider
		b.Client.HTTP = &http.Client{Timeout: cfg.LLMTimeout()}
	}
}

func (b *Brain) Close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
	}
}

func (b *Brain) Tail(channel string, n int) string {
	if n <= 0 {
		return ""
	}
	b.mu.Lock()
	hist := append([]HistoryLine{}, b.hist[irccase.Fold(channel)]...)
	b.mu.Unlock()
	if len(hist) > n {
		hist = hist[len(hist)-n:]
	}
	var sb strings.Builder
	for _, h := range hist {
		sb.WriteString("<")
		sb.WriteString(h.Nick)
		sb.WriteString("> ")
		sb.WriteString(h.Text)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (b *Brain) Remember(channel, nick, text string) {
	if text == "" {
		return
	}
	ch := irccase.Fold(channel)
	b.mu.Lock()
	defer b.mu.Unlock()
	max := b.Cfg.LLM.Limits.HistoryLines
	if max <= 0 {
		max = 40
	}
	b.hist[ch] = append(b.hist[ch], HistoryLine{Nick: nick, Text: text, At: time.Now()})
	if len(b.hist[ch]) > max {
		b.hist[ch] = b.hist[ch][len(b.hist[ch])-max:]
	}
}

type AskReq struct {
	Channel  string
	Nick     string
	Handle   string
	User     *userfile.User
	Persona  string
	Prompt   string
	Topic    string
	Dest     string // DestChannel, DestDM, DestCatchup, DestSearch, DestHistory
	gen      uint64 // per-nick generation; a newer ask makes this one stale
	Helpful  bool   // seen/quote/dm tools
	Ops      bool   // kick/ban/topic tools
	Search   bool   // xAI web_search (server-side)
	OnStart  func() // fired when this job actually begins (not when queued)
	Admitted bool   // rate/capacity was reserved before optional routing
	FollowUp bool   // sticky follow-up without a prefix; only these may be SILENT or DROP
}

func (b *Brain) Ask(channel, nick, handle string, u *userfile.User, persona, prompt string, toolsOK bool) (string, error) {
	return b.AskReq(AskReq{
		Channel: channel, Nick: nick, Handle: handle, User: u,
		Persona: persona, Prompt: prompt, Dest: DestChannel, Ops: toolsOK, Helpful: true,
	})
}

// AskReq runs one ask. If that nick is already in flight, the job waits on a
// per-nick FIFO (max maxAskQueue). History is whatever Remember has stored
// since process start.
func (b *Brain) AskReq(req AskReq) (string, error) {
	if !req.Admitted {
		if err := b.Admit(&req); err != nil {
			return "", err
		}
	}
	key := flightKey(req)
	b.mu.Lock()
	b.gen[key]++
	req.gen = b.gen[key]
	if b.inflight[key] {
		for _, old := range b.queue[key] {
			if b.jobs > 0 {
				b.jobs--
			}
			old.done <- askResult{err: ErrStale}
		}
		job := askJob{req: req, done: make(chan askResult, 1)}
		b.queue[key] = []askJob{job}
		b.mu.Unlock()
		res := <-job.done
		return res.text, res.err
	}
	b.inflight[key] = true
	myGen := req.gen
	b.mu.Unlock()

	text, err := b.answer(req)
	b.finishFlight(key)
	if b.stale(key, myGen) {
		return "", ErrStale
	}
	return text, err
}

func (b *Brain) stale(key string, gen uint64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return gen != 0 && b.gen[key] != gen
}

// Admit reserves rate and global capacity before the optional router makes an
// external request. Callers must pass the admitted request to AskReq.
func (b *Brain) Admit(req *AskReq) error {
	if req.Admitted {
		return nil
	}
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return fmt.Errorf("llm shutting down")
	}
	if !b.Cfg.LLM.Enabled {
		return fmt.Errorf("llm disabled")
	}
	if b.Cfg.LLM.APIKey == "" {
		return fmt.Errorf("no API key (export %s)", b.Cfg.LLM.APIKeyEnv)
	}
	b.mu.Lock()
	if b.jobs >= maxAskJobs {
		b.mu.Unlock()
		return fmt.Errorf("AI is busy, try again shortly")
	}
	b.jobs++
	b.mu.Unlock()
	if err := b.rateOK(*req); err != nil {
		b.mu.Lock()
		b.jobs--
		b.mu.Unlock()
		return err
	}
	req.Admitted = true
	return nil
}

func (b *Brain) finishFlight(key string) {
	b.mu.Lock()
	if b.jobs > 0 {
		b.jobs--
	}
	q := b.queue[key]
	if len(q) == 0 {
		delete(b.inflight, key)
		delete(b.queue, key)
		delete(b.searchAck, key)
		b.mu.Unlock()
		return
	}
	next := q[0]
	b.queue[key] = q[1:]
	b.mu.Unlock()
	go b.runQueued(key, next)
}

// ClaimSearchAck is true once per nick/handle busy session (inflight + queue).
func (b *Brain) ClaimSearchAck(handle, nick string) bool {
	key := flightKey(AskReq{Handle: handle, Nick: nick})
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.inflight[key] || b.searchAck[key] {
		return false
	}
	b.searchAck[key] = true
	return true
}

func (b *Brain) runQueued(key string, job askJob) {
	text, err := b.answer(job.req)
	b.finishFlight(key)
	if b.stale(key, job.req.gen) {
		job.done <- askResult{err: ErrStale}
		return
	}
	job.done <- askResult{text: text, err: err}
}

func (b *Brain) answer(req AskReq) (string, error) {
	if req.OnStart != nil {
		req.OnStart()
	}

	hist := b.historyFor(req)
	ghNote, ghOK := b.lookupGitHub(req, hist)
	useSearch := req.Search && req.Dest != DestCatchup && req.Dest != DestHistory && !ghOK
	msgs := b.buildMessages(req, hist, ghNote, useSearch)
	searchX := useSearch && wantsXSearch(req.Prompt)

	var specs []ToolSpec
	var allowed []ToolDef
	for _, t := range b.Tools {
		if t.Kind == "ops" && !req.Ops {
			continue
		}
		if t.Kind == "help" && !req.Helpful && req.Dest != DestDM {
			continue
		}
		if req.Dest == DestCatchup || req.Dest == DestHistory {
			continue
		}
		// Tools run as the asking user. Injection cannot raise their flags.
		if t.MinFlags != "" && t.MinFlags != "-" && (req.Handle == "" || b.Actor == nil || !b.Actor.MatchAttr(req.Handle, t.MinFlags, req.Channel)) {
			continue
		}
		specs = append(specs, t.Spec)
		allowed = append(allowed, t)
	}

	steps := b.Cfg.LLM.Limits.MaxToolSteps
	if steps <= 0 {
		steps = 4
	}
	if useSearch {
		// Live lookup is one xAI /responses round with server-side web search.
		// Extra bot-tool loops are what made !search sit on "Searching..." for a minute.
		specs = nil
		allowed = nil
		steps = 0
	}
	maxChars := b.Cfg.LLM.Limits.MaxChannelChars
	if req.Dest == DestDM || req.Dest == DestCatchup {
		maxChars = b.Cfg.LLM.Limits.MaxDMChars
	}
	if req.Dest == DestSearch && (maxChars <= 0 || maxChars > searchMaxChars) {
		maxChars = searchMaxChars
	}
	if maxChars <= 0 {
		maxChars = b.Cfg.LLM.Limits.MaxOutputChars
	}
	var prevID string
	for i := 0; i < steps+1; i++ {
		var (
			content string
			calls   []ToolCall
			err     error
		)
		if useSearch {
			in := msgs
			if prevID != "" {
				var extras []Message
				for _, m := range msgs {
					if m.Role == "tool" {
						extras = append(extras, m)
					}
				}
				in = extras
			}
			content, calls, prevID, err = b.Client.Respond(in, specs, true, searchX, prevID)
		} else {
			var msg Message
			maxTok := 0
			if req.Dest == DestChannel || req.Dest == DestSearch || req.Dest == DestHistory {
				maxTok = 180
			}
			msg, err = b.Client.chat(b.Client.Model, msgs, specs, maxTok)
			content, calls = msg.Content, msg.ToolCalls
		}
		if err != nil {
			if b.Log != nil && useSearch {
				b.Log.Error("live lookup failed", "err", err, "step", i)
			}
			return "", friendlyErr(err)
		}
		if len(calls) == 0 {
			return clip(content, maxChars), nil
		}
		if !useSearch {
			// keep chat-completions history
			msgs = append(msgs, Message{Role: "assistant", Content: content, ToolCalls: calls})
		} else {
			msgs = nil // next iter only tool outputs + prevID
		}
		for _, tc := range calls {
			args := ParseArgs(tc.Function.Arguments)
			out, err := b.runTool(allowed, tc.Function.Name, ToolCtx{Handle: req.Handle, Channel: req.Channel, Nick: req.Nick}, args)
			if err != nil {
				out = "error: " + err.Error()
			}
			msgs = append(msgs, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    normalizeIRC(out),
			})
		}
	}
	return "i ran out of tool steps", nil
}

func wantsXSearch(prompt string) bool {
	s := strings.ToLower(prompt)
	for _, w := range []string{"twitter", "tweet", "x.com", " on x", "x post", "x search"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

func flightKey(req AskReq) string {
	if req.Handle != "" {
		return "h:" + irccase.Fold(req.Handle)
	}
	return "n:" + irccase.Fold(req.Nick)
}

func (b *Brain) runTool(allowed []ToolDef, name string, ctx ToolCtx, args map[string]any) (string, error) {
	for _, t := range allowed {
		if t.Spec.Function.Name == name {
			if t.MinFlags != "" && t.MinFlags != "-" && b.Actor != nil {
				if !b.Actor.MatchAttr(ctx.Handle, t.MinFlags, ctx.Channel) {
					return "", fmt.Errorf("you lack +%s", t.MinFlags)
				}
			}
			if b.Actor != nil {
				return b.Actor.RunTool(name, ctx, args)
			}
			if t.Run != nil {
				return t.Run(ctx, args)
			}
		}
	}
	return "", fmt.Errorf("unknown or forbidden tool %s", name)
}

func (b *Brain) rateOK(req AskReq) error {
	if b.Cfg.LLM.Limits.OwnerUnlimited && req.User != nil {
		if req.User.Global.Has(flags.Owner) || req.User.Global.Has(flags.Master) {
			return nil
		}
	}
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	cutoff := now.Add(-time.Minute)
	pruneRateMap(b.userN, cutoff)
	pruneRateMap(b.chanN, cutoff)

	uk := irccase.Fold(req.Handle)
	if uk == "" {
		uk = "nick:" + irccase.Fold(req.Nick)
	}
	userLimit := b.Cfg.LLM.Limits.PerUserPerMin
	if req.User != nil && (req.User.Global.Has(flags.Op) || (req.Channel != "" && req.User.ChanFlags[irccase.Fold(req.Channel)].Has(flags.Op))) {
		userLimit = b.Cfg.LLM.Limits.OpPerMin
	}
	if userLimit > 0 && len(b.userN[uk]) >= userLimit {
		msg := fmt.Sprintf("rate limit (%d/min)", userLimit)
		if userLimit <= 1 {
			msg = "one AI ask per minute — owners/ops get more"
		}
		return &RateLimitError{Scope: "user", Limit: userLimit, RetryAt: nextSlot(b.userN[uk], userLimit), msg: msg}
	}
	ch := irccase.Fold(req.Channel)
	if ch == "" {
		ch = "priv"
	}
	channelLimit := b.Cfg.LLM.Limits.PerChannelPerMin
	if channelLimit > 0 && len(b.chanN[ch]) >= channelLimit {
		return &RateLimitError{Scope: "channel", Limit: channelLimit, RetryAt: nextSlot(b.chanN[ch], channelLimit), msg: "channel is AI-busy, try in a minute"}
	}
	if userLimit > 0 && len(b.userN[uk]) == 0 && len(b.userN) >= maxRateKeys {
		return fmt.Errorf("AI is busy, try again shortly")
	}
	if channelLimit > 0 && len(b.chanN[ch]) == 0 && len(b.chanN) >= maxRateKeys {
		return fmt.Errorf("AI is busy, try again shortly")
	}
	if userLimit > 0 {
		b.userN[uk] = append(b.userN[uk], now)
	}
	if channelLimit > 0 {
		b.chanN[ch] = append(b.chanN[ch], now)
	}
	return nil
}

func nextSlot(entries []time.Time, limit int) time.Time {
	if limit <= 0 || len(entries) == 0 {
		return time.Now().Add(time.Minute)
	}
	i := 0
	if len(entries) > limit {
		i = len(entries) - limit
	}
	return entries[i].Add(time.Minute)
}

func pruneRateMap(m map[string][]time.Time, cutoff time.Time) {
	for key, entries := range m {
		kept := entries[:0]
		for _, entry := range entries {
			if entry.After(cutoff) {
				kept = append(kept, entry)
			}
		}
		if len(kept) == 0 {
			delete(m, key)
		} else {
			m[key] = kept
		}
	}
}

func clip(s string, n int) string {
	s = normalizeIRC(s)
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
	s = s[:cut]
	if i := strings.LastIndex(s, "http"); i >= 0 {
		rest := s[i:]
		if !strings.Contains(rest, " ") && !strings.Contains(rest, "://") {
			s = strings.TrimSpace(s[:i])
		} else if !strings.Contains(rest, " ") {
			// possibly a truncated URL — drop the broken tail
			if strings.Count(rest, "/") < 2 {
				s = strings.TrimSpace(s[:i])
			}
		}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return s + "…"
}

// FlagCheck is a tiny helper so tools can reuse matchattr.
func FlagCheck(u *userfile.User, expr, channel string) bool {
	if u == nil {
		return flags.MatchAttr(nil, nil, expr)
	}
	var ch flags.Set
	if channel != "" {
		ch = u.ChanFlags[irccase.Fold(channel)]
	}
	return flags.MatchAttr(u.Global, ch, expr)
}
