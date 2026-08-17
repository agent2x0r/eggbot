package bot

import (
	"context"
	"fmt"
	"time"

	"eggbot/internal/chanstate"
	"eggbot/internal/irccase"
	"eggbot/internal/llm"
	"eggbot/internal/store"
)

func (b *Bot) toolDefs() []llm.ToolDef {
	return []llm.ToolDef{
		{Spec: llm.FuncTool("dm", "Send a private IRC message (extra links). Default target is the asking nick.", llm.Schema(map[string]any{
			"nick": llm.Prop("string", "Nick to query. Omit to message the asker."),
			"text": llm.Prop("string", "Short private text, may include URLs"),
		}, []string{"text"})), MinFlags: "-", Kind: "help"},
		{Spec: llm.FuncTool("channel_say", "Send a message to the channel", llm.Schema(map[string]any{
			"text": llm.Prop("string", "What to say"),
		}, []string{"text"})), MinFlags: "o", Kind: "ops"},
		{Spec: llm.FuncTool("channel_act", "Send a /me action to the channel", llm.Schema(map[string]any{
			"text": llm.Prop("string", "Action text"),
		}, []string{"text"})), MinFlags: "o", Kind: "ops"},
		{Spec: llm.FuncTool("set_topic", "Set the channel topic", llm.Schema(map[string]any{
			"topic": llm.Prop("string", "New topic"),
		}, []string{"topic"})), MinFlags: "o", Kind: "ops"},
		{Spec: llm.FuncTool("kick", "Kick a nick from the channel", llm.Schema(map[string]any{
			"nick":   llm.Prop("string", "Nick to kick"),
			"reason": llm.Prop("string", "Kick reason"),
		}, []string{"nick"})), MinFlags: "o", Kind: "ops"},
		{Spec: llm.FuncTool("ban", "Ban a hostmask and optionally kick", llm.Schema(map[string]any{
			"mask":   llm.Prop("string", "Ban mask like nick!user@host"),
			"reason": llm.Prop("string", "Reason"),
			"kick":   llm.Prop("boolean", "Also kick matching nick"),
		}, []string{"mask"})), MinFlags: "o", Kind: "ops"},
		{Spec: llm.FuncTool("unban", "Remove a ban mask", llm.Schema(map[string]any{
			"mask": llm.Prop("string", "Ban mask to remove"),
		}, []string{"mask"})), MinFlags: "o", Kind: "ops"},
		{Spec: llm.FuncTool("mode", "Set limited channel modes", llm.Schema(map[string]any{
			"modes": llm.Prop("string", "Mode string like +v or -o"),
			"nick":  llm.Prop("string", "Target nick or mask if needed"),
		}, []string{"modes"})), MinFlags: "o", Kind: "ops"},
		{Spec: llm.FuncTool("seen_lookup", "Look up when a nick was last seen", llm.Schema(map[string]any{
			"nick": llm.Prop("string", "Nick to look up"),
		}, []string{"nick"})), MinFlags: "-", Kind: "help"},
		{Spec: llm.FuncTool("quote_search", "Fetch a random or matching quote", llm.Schema(map[string]any{
			"search": llm.Prop("string", "Optional search text"),
		}, []string{})), MinFlags: "-", Kind: "help"},
		{Spec: llm.FuncTool("github_repo_lookup", "Read a public GitHub repository's latest release and changelog. Prefer this over web search for GitHub URLs in the question, topic, or scrollback.", llm.Schema(map[string]any{
			"url":  llm.Prop("string", "github.com URL or owner/repo"),
			"repo": llm.Prop("string", "owner/repo if url is omitted"),
		}, []string{})), MinFlags: "-", Kind: "help"},
		{Spec: llm.FuncTool("note_add", "Leave a note for a user handle", llm.Schema(map[string]any{
			"handle": llm.Prop("string", "Recipient handle"),
			"text":   llm.Prop("string", "Note body"),
		}, []string{"handle", "text"})), MinFlags: "p", Kind: "help"},
		{Spec: llm.FuncTool("user_lookup", "Look up a user's flags (redacted)", llm.Schema(map[string]any{
			"handle": llm.Prop("string", "Handle or nick"),
		}, []string{"handle"})), MinFlags: "m", Kind: "ops"},
		{Spec: llm.FuncTool("chanset", "Change a documented channel setting", llm.Schema(map[string]any{
			"spec": llm.Prop("string", "+flag or -flag from the known chanset list"),
		}, []string{"spec"})), MinFlags: "m", Kind: "ops"},
	}
}

func (b *Bot) RunTool(name string, ctx llm.ToolCtx, args map[string]any) (string, error) {
	if b.Cfg.LLM.ConfirmMutations && mutatingTool(name) {
		return b.proposeTool(ctx.Handle, ctx.Channel, name, args)
	}
	ch := ctx.Channel
	res, err := b.execTool(name, ctx, args)
	status := res
	if err != nil {
		status = "error: " + err.Error()
	}
	_, _ = b.Store.DB.Exec(
		`INSERT INTO ai_audit (handle, channel, tool, args, result, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		ctx.Handle, ch, name, fmt.Sprint(args), status, store.Now(),
	)
	b.PL.Consolef('c', "[ai] %s/%s %s %v -> %s", ctx.Handle, name, ch, args, status)
	return res, err
}

func mutatingTool(name string) bool {
	switch name {
	case "kick", "ban", "unban", "mode", "set_topic", "chanset":
		return true
	default:
		return false
	}
}

func (b *Bot) execTool(name string, ctx llm.ToolCtx, args map[string]any) (string, error) {
	if err := b.authorizeTool(name, ctx.Handle, ctx.Channel); err != nil {
		return "", err
	}
	ch := ctx.Channel
	switch name {
	case "dm":
		target := llm.Str(args, "nick")
		if target == "" {
			target = ctx.Nick
		}
		if !irccase.Equal(target, ctx.Nick) {
			if ctx.Handle == "" || !b.MatchAttr(ctx.Handle, "o", ch) {
				return "", fmt.Errorf("can only DM yourself unless you have +o")
			}
		}
		text := llm.Str(args, "text")
		if text == "" {
			return "", fmt.Errorf("empty dm")
		}
		b.IRC.PrivmsgHelp(target, text)
		return "sent to " + target, nil
	case "channel_say":
		if ch == "" {
			return "", fmt.Errorf("no channel")
		}
		b.IRC.Privmsg(ch, llm.Str(args, "text"))
		return "said", nil
	case "channel_act":
		if ch == "" {
			return "", fmt.Errorf("no channel")
		}
		b.IRC.Action(ch, llm.Str(args, "text"))
		return "acted", nil
	case "set_topic":
		if ch == "" {
			return "", fmt.Errorf("no channel")
		}
		b.IRC.Topic(ch, llm.Str(args, "topic"))
		return "topic set", nil
	case "kick":
		if ch == "" {
			return "", fmt.Errorf("no channel")
		}
		reason := llm.Str(args, "reason")
		if reason == "" {
			reason = "requested"
		}
		b.IRC.Kick(ch, llm.Str(args, "nick"), reason)
		return "kicked", nil
	case "ban":
		if ch == "" {
			return "", fmt.Errorf("no channel")
		}
		mask := llm.Str(args, "mask")
		reason := llm.Str(args, "reason")
		_ = b.Chans.AddBan(chanstate.Ban{Channel: ch, Mask: mask, Reason: reason, Setter: ctx.Handle, CreatedAt: time.Now()})
		b.IRC.Mode(ch, "+b", mask)
		return "banned " + mask, nil
	case "unban":
		if ch == "" {
			return "", fmt.Errorf("no channel")
		}
		mask := llm.Str(args, "mask")
		_ = b.Chans.DelBan(ch, mask)
		b.IRC.Mode(ch, "-b", mask)
		return "unbanned " + mask, nil
	case "mode":
		if ch == "" {
			return "", fmt.Errorf("no channel")
		}
		modes := llm.Str(args, "modes")
		if !allowedModes(modes) {
			return "", fmt.Errorf("mode not allowed")
		}
		nick := llm.Str(args, "nick")
		if nick != "" {
			b.IRC.Mode(ch, modes, nick)
		} else {
			b.IRC.Mode(ch, modes)
		}
		return "mode " + modes, nil
	case "seen_lookup":
		r, err := b.Seen.Lookup(llm.Str(args, "nick"))
		if err != nil {
			return "", err
		}
		return b.Seen.Format(llm.Str(args, "nick"), r), nil
	case "quote_search":
		q, err := b.Quotes.Random(ch, llm.Str(args, "search"))
		if err != nil {
			return "", err
		}
		if q == nil {
			return "no quotes", nil
		}
		return q.Format(), nil
	case "github_repo_lookup":
		spec := llm.Str(args, "url")
		if spec == "" {
			spec = llm.Str(args, "repo")
		}
		if spec == "" {
			return "", fmt.Errorf("missing url")
		}
		if b.GitHub == nil {
			return "", fmt.Errorf("github lookup unavailable")
		}
		info, err := b.GitHub.Lookup(context.Background(), spec)
		if err != nil {
			return "", err
		}
		return info.Format(), nil
	case "note_add":
		if err := b.Notes.Add(llm.Str(args, "handle"), ctx.Handle, llm.Str(args, "text")); err != nil {
			return "", err
		}
		return "noted", nil
	case "user_lookup":
		u, err := b.Users.Get(llm.Str(args, "handle"))
		if err != nil {
			if found := b.Users.FindByHost(llm.Str(args, "handle") + "!*@*"); found != nil {
				u = found
			} else {
				return "unknown user", nil
			}
		}
		return fmt.Sprintf("%s +%s", u.Handle, u.Global.String()), nil
	case "chanset":
		if ch == "" {
			return "", fmt.Errorf("no channel")
		}
		c, err := b.Chans.ApplyChanSet(ch, llm.Str(args, "spec"))
		if err != nil {
			return "", err
		}
		return c.Chanset.String(), nil
	default:
		return "", fmt.Errorf("unknown tool")
	}
}

func allowedModes(modes string) bool {
	sign := byte(0)
	for i := 0; i < len(modes); i++ {
		switch modes[i] {
		case '+', '-':
			sign = modes[i]
		case 'v', 'o', 'b', 't', 'n', 'i', 'm':
			if sign == 0 {
				return false
			}
		default:
			if modes[i] != ' ' {
				return false
			}
		}
	}
	return sign != 0
}

func (b *Bot) authorizeTool(name, handle, channel string) error {
	for _, d := range b.toolDefs() {
		if d.Spec.Function.Name != name {
			continue
		}
		return b.authorize(handle, d.MinFlags, channel)
	}
	return fmt.Errorf("unknown tool")
}
