package bot

import (
	"eggbot/internal/llm"
)

const chatHistKVKey = "llm.chat_hist"

func (b *Bot) saveChatHist() {
	if b == nil || b.LLM == nil || b.Store == nil {
		return
	}
	raw, err := b.LLM.DumpHist(b.now())
	if err != nil {
		if b.Log != nil {
			b.Log.Error("chat hist dump", "err", err)
		}
		b.Store.NoteWriteError()
		return
	}
	if len(raw) == 0 {
		_ = b.Store.DelKV(chatHistKVKey)
		return
	}
	if err := b.Store.PutKV(chatHistKVKey, string(raw)); err != nil {
		if b.Log != nil {
			b.Log.Error("chat hist save", "err", err)
		}
		b.Store.NoteWriteError()
	}
}

func (b *Bot) restoreChatHist() {
	if b == nil || b.LLM == nil || b.Store == nil {
		return
	}
	raw, err := b.Store.GetKV(chatHistKVKey)
	if err != nil {
		if b.Log != nil {
			b.Log.Error("chat hist load", "err", err)
		}
		return
	}
	if raw == "" {
		return
	}
	n, err := b.LLM.RestoreHist([]byte(raw), b.now())
	if err != nil {
		if b.Log != nil {
			b.Log.Error("chat hist restore", "err", err)
		}
		_ = b.Store.DelKV(chatHistKVKey)
		return
	}
	if n == 0 {
		_ = b.Store.DelKV(chatHistKVKey)
		if b.Log != nil {
			b.Log.Info("chat hist snapshot too old, starting empty")
		}
		return
	}
	if b.Log != nil {
		b.Log.Info("restored chat hist", "lines", n, "max_age", llm.ChatHistMaxAge)
	}
}
