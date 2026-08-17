package config

import "fmt"

// Change is one setting difference between two configs.
type Change struct {
	Path string
	Hot  bool
	From string
	To   string
}

// Diff compares loaded configs. Hot settings may be applied in-process.
// Cold settings require a restart.
func Diff(old, next *Config) []Change {
	if old == nil || next == nil {
		return nil
	}
	var out []Change
	cold := func(path, a, b string) {
		if a != b {
			out = append(out, Change{Path: path, Hot: false, From: a, To: b})
		}
	}
	hot := func(path, a, b string) {
		if a != b {
			out = append(out, Change{Path: path, Hot: true, From: a, To: b})
		}
	}
	cold("store.path", old.Store.Path, next.Store.Path)
	cold("server.host", old.Server.Host, next.Server.Host)
	cold("server.port", fmt.Sprint(old.Server.Port), fmt.Sprint(next.Server.Port))
	cold("server.tls", fmt.Sprint(old.Server.TLS), fmt.Sprint(next.Server.TLS))
	cold("server.sasl_account", old.Server.SASLAccount, next.Server.SASLAccount)
	cold("partyline.listen", old.Partyline.Listen, next.Partyline.Listen)
	cold("partyline.tls_listen", old.Partyline.TLSListen, next.Partyline.TLSListen)
	hot("log.level", old.Log.Level, next.Log.Level)
	hot("llm.enabled", fmt.Sprint(old.LLM.Enabled), fmt.Sprint(next.LLM.Enabled))
	hot("llm.model", old.LLM.Model, next.LLM.Model)
	hot("llm.base_url", old.LLM.BaseURL, next.LLM.BaseURL)
	hot("llm.confirm_mutations", fmt.Sprint(old.LLM.ConfirmMutations), fmt.Sprint(next.LLM.ConfirmMutations))
	hot("scripts.lua_dir", old.Scripts.LuaDir, next.Scripts.LuaDir)
	hot("scripts.python", fmt.Sprint(old.Scripts.PythonEnabled), fmt.Sprint(next.Scripts.PythonEnabled))
	hot("learn.hello", fmt.Sprint(old.Learn.Hello), fmt.Sprint(next.Learn.Hello))
	hot("observe.json_logs", fmt.Sprint(old.Observe.JSONLogs), fmt.Sprint(next.Observe.JSONLogs))
	return out
}
