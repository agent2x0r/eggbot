package bot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"eggbot/internal/llm"
	"eggbot/internal/store"
)

type Proposal struct {
	ID      string
	Handle  string
	Channel string
	Tool    string
	Args    map[string]any
	Expires time.Time
}

func (b *Bot) expireLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.expireBans()
			b.expireProposals()
			if days := b.Cfg.Store.RetentionDays; days > 0 {
				_ = b.Store.Purge(ctx, days)
			}
			if days := b.Cfg.Store.ChanlogDays; days > 0 {
				_ = b.Store.PurgeChanlog(ctx, days)
			}
			b.saveChatHist()
			if _, err := b.Store.Checkpoint(ctx, store.CheckpointPassive); err != nil && b.Log != nil {
				b.Log.Error("wal checkpoint", "err", err)
			}
			b.resumeDueSticky(b.now())
		}
	}
}

func (b *Bot) expireBans() {
	for _, ban := range b.Chans.AllBans() {
		if ban.Expired() {
			_ = b.Chans.DelBan(ban.Channel, ban.Mask)
			b.IRC.Mode(ban.Channel, "-b", ban.Mask)
		}
	}
}

func (b *Bot) expireProposals() {
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, p := range b.proposals {
		if now.After(p.Expires) {
			delete(b.proposals, id)
		}
	}
}

func (b *Bot) proposeTool(handle, channel, tool string, args map[string]any) (string, error) {
	id := newID()
	p := &Proposal{
		ID: id, Handle: handle, Channel: channel, Tool: tool, Args: args,
		Expires: time.Now().Add(5 * time.Minute),
	}
	b.mu.Lock()
	if b.proposals == nil {
		b.proposals = map[string]*Proposal{}
	}
	b.proposals[id] = p
	b.mu.Unlock()
	raw, _ := json.Marshal(args)
	_, _ = b.Store.DB.Exec(
		`INSERT INTO proposals (id, handle, channel, tool, args, created_at, expires_at, status) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending')`,
		id, handle, channel, tool, string(raw), store.Now(), p.Expires.Unix(),
	)
	return fmt.Sprintf("proposed %s on %s; confirm with .confirm %s", tool, channel, id), nil
}

func (b *Bot) ConfirmProposal(id, actor string) (string, error) {
	b.mu.Lock()
	p := b.proposals[id]
	b.mu.Unlock()
	if p == nil || time.Now().After(p.Expires) {
		return "", fmt.Errorf("no such proposal")
	}
	if !strings.EqualFold(p.Handle, actor) {
		u, err := b.Users.Get(actor)
		if err != nil || !(u.Global.Has('n') || u.Global.Has('m')) {
			return "", fmt.Errorf("not your proposal")
		}
	}
	if err := b.authorizeTool(p.Tool, p.Handle, p.Channel); err != nil {
		return "", err
	}
	res, err := b.execTool(p.Tool, llm.ToolCtx{Handle: p.Handle, Channel: p.Channel}, p.Args)
	b.mu.Lock()
	delete(b.proposals, id)
	b.mu.Unlock()
	st := "confirmed"
	if err != nil {
		st = "error"
	}
	_, _ = b.Store.DB.Exec(`UPDATE proposals SET status = ? WHERE id = ?`, st, id)
	return res, err
}

func (b *Bot) RejectProposal(id, _ string) error {
	b.mu.Lock()
	if _, ok := b.proposals[id]; !ok {
		b.mu.Unlock()
		return fmt.Errorf("no such proposal")
	}
	delete(b.proposals, id)
	b.mu.Unlock()
	_, _ = b.Store.DB.Exec(`UPDATE proposals SET status = 'rejected' WHERE id = ?`, id)
	return nil
}

func newID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
