// Package config loads eggbot.toml and fills LLM/SASL secrets from the environment.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

type Config struct {
	Nick      string             `toml:"nick"`
	AltNicks  []string           `toml:"alt_nicks"`
	Username  string             `toml:"username"`
	Realname  string             `toml:"realname"`
	Server    Server             `toml:"server"`
	Partyline Partyline          `toml:"partyline"`
	Owners    Owners             `toml:"owners"`
	Learn     Learn              `toml:"learn"`
	Channels  Channels           `toml:"channels"`
	Channel   map[string]Channel `toml:"channel"`
	LLM       LLM                `toml:"llm"`
	Scripts   Scripts            `toml:"scripts"`
	Store     Store              `toml:"store"`
	Log       Log                `toml:"log"`
	Observe   Observe            `toml:"observe"`
	Path      string             `toml:"-"`
}

type Observe struct {
	Listen   string `toml:"listen"`
	JSONLogs bool   `toml:"json_logs"`
}

// DefaultPersona is used when llm.persona is empty. Voice only — policy is
// appended by the llm package so it cannot be configured away.
const DefaultPersona = "You are eggbot, a regular on this IRC channel who happens to know things. Plain, brief, a little dry. No markdown, no catchphrases, no addressing people by nick. Stay out of fights."

type Server struct {
	Host               string   `toml:"host"`
	Port               int      `toml:"port"`
	TLS                bool     `toml:"tls"`
	Password           string   `toml:"password"`
	PasswordEnv        string   `toml:"password_env"`
	SASLAccount        string   `toml:"sasl_account"`
	SASLPassword       string   `toml:"sasl_password"`
	SASLPasswordEnv    string   `toml:"sasl_password_env"`
	InsecureSkipVerify bool     `toml:"insecure_skip_verify"`
	RequestCaps        []string `toml:"request_caps"`
	Network            string   `toml:"network"`
	Endpoints          []string `toml:"endpoints"`
	TLSServerName      string   `toml:"tls_server_name"`
	CAFile             string   `toml:"ca_file"`
	ClientCert         string   `toml:"client_cert"`
	ClientKey          string   `toml:"client_key"`
	DialTimeoutSec     int      `toml:"dial_timeout_sec"`
}

type Partyline struct {
	Listen    string `toml:"listen"`
	TLSListen string `toml:"tls_listen"`
	TLSCert   string `toml:"tls_cert"`
	TLSKey    string `toml:"tls_key"`
	DCC       bool   `toml:"dcc"`
}

type Owners struct {
	Handles        []string `toml:"handles"`
	AutoOwnerHosts []string `toml:"auto_owner_hosts"`
}

type Learn struct {
	Hello          bool   `toml:"hello"`
	HelloFlags     string `toml:"hello_flags"`
	HelloPerHour   int    `toml:"hello_per_hour"`
	ProductionLock bool   `toml:"production"`
}

type Channels struct {
	Join []string `toml:"join"`
}

type Channel struct {
	Chanset    string `toml:"chanset"`
	LLMPersona string `toml:"llm_persona"`
	Greet      string `toml:"greet"`
	NeedOp     string `toml:"need_op"`
	Key        string `toml:"key"`
	KeyEnv     string `toml:"key_env"`
}

type LLM struct {
	Enabled          bool   `toml:"enabled"`
	BaseURL          string `toml:"base_url"`
	APIKey           string `toml:"api_key"`
	APIKeyEnv        string `toml:"api_key_env"`
	Model            string `toml:"model"`
	SearchModel      string `toml:"search_model"`     // live !search; empty = model
	SearchReasoning  string `toml:"search_reasoning"` // Responses reasoning.effort; empty = omit
	AllowPrivmsg     bool   `toml:"allow_privmsg"`
	Search           bool   `toml:"search"`
	RouteModel       string `toml:"route_model"` // cheap/fast model for SEARCH vs LOCAL
	Persona          string `toml:"persona"`
	StoreProvider    bool   `toml:"store_provider"`
	ConfirmMutations bool   `toml:"confirm_mutations"`
	Sticky           bool   `toml:"sticky"` // unprefixed follow-ups after an ask
	Limits           Limits `toml:"limits"`
}

type Limits struct {
	PerUserPerMin    int  `toml:"per_user_per_min"`    // regulars
	OpPerMin         int  `toml:"op_per_min"`          // +o
	OwnerUnlimited   bool `toml:"owner_unlimited"`     // +n / +m skip user+channel caps
	PerChannelPerMin int  `toml:"per_channel_per_min"` // pile-on brake (ignored for owners)
	MaxChannelChars  int  `toml:"max_channel_chars"`
	MaxDMChars       int  `toml:"max_dm_chars"`
	MaxOutputChars   int  `toml:"max_output_chars"` // fallback
	HistoryLines     int  `toml:"history_lines"`
	TimeoutSec       int  `toml:"timeout_sec"` // chat and live search
	MaxToolSteps     int  `toml:"max_tool_steps"`
}

type Scripts struct {
	LuaDir        string   `toml:"lua_dir"`
	PythonDir     string   `toml:"python_dir"`
	PythonBin     string   `toml:"python_bin"`
	PythonEnabled bool     `toml:"python"`
	Trusted       []string `toml:"trusted"`
}

type Store struct {
	Path          string `toml:"path"`
	RetentionDays int    `toml:"retention_days"`
	ChanlogDays   int    `toml:"chanlog_days"` // public channel lines for !history / future RAG
}

type Log struct {
	Level    string `toml:"level"`
	DebugIRC bool   `toml:"debug_irc"`
}

func Defaults() *Config {
	return &Config{
		Nick:     "eggbot",
		AltNicks: []string{"eggbot_", "eggbot2"},
		Username: "eggbot",
		Realname: "an egg with a brain",
		Server: Server{
			Port: 6697,
			TLS:  true,
		},
		Partyline: Partyline{
			Listen: "127.0.0.1:3333",
			DCC:    false,
		},
		Learn:   Learn{Hello: false, HelloPerHour: 10},
		Channel: map[string]Channel{},
		LLM: LLM{
			Enabled:          true,
			BaseURL:          "https://api.x.ai/v1",
			APIKeyEnv:        "XAI_API_KEY",
			Model:            "grok-4.6",
			SearchModel:      "grok-4.20-0309-non-reasoning",
			SearchReasoning:  "",
			Search:           true,
			Persona:          DefaultPersona,
			ConfirmMutations: true,
			Sticky:           true,
			StoreProvider:    false,
			Limits: Limits{
				PerUserPerMin:    1,
				OpPerMin:         4,
				OwnerUnlimited:   true,
				PerChannelPerMin: 8,
				MaxChannelChars:  800,
				MaxDMChars:       1200,
				MaxOutputChars:   800,
				HistoryLines:     80,
				TimeoutSec:       60,
				MaxToolSteps:     4,
			},
		},
		Scripts: Scripts{
			LuaDir:    "scripts/lua",
			PythonDir: "scripts/python",
			PythonBin: "python3",
		},
		Store:   Store{Path: "eggbot.db", RetentionDays: 90, ChanlogDays: 365},
		Log:     Log{Level: "info"},
		Observe: Observe{Listen: "127.0.0.1:0"},
	}
}

func Load(path string) (*Config, error) {
	cfg := Defaults()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := toml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("config path: %w", err)
	}
	cfg.resolveRelPaths(filepath.Dir(abs))
	cfg.applyEnv()
	cfg.Path = abs
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// resolveRelPaths makes script dirs, the sqlite file, and TLS material
// relative to the toml so `eggbot -c /path/to/eggbot.toml` works from any cwd.
func (c *Config) resolveRelPaths(base string) {
	c.Scripts.LuaDir = relTo(base, c.Scripts.LuaDir)
	c.Scripts.PythonDir = relTo(base, c.Scripts.PythonDir)
	c.Store.Path = relTo(base, c.Store.Path)
	c.Partyline.TLSCert = relTo(base, c.Partyline.TLSCert)
	c.Partyline.TLSKey = relTo(base, c.Partyline.TLSKey)
	c.Server.CAFile = relTo(base, c.Server.CAFile)
	c.Server.ClientCert = relTo(base, c.Server.ClientCert)
	c.Server.ClientKey = relTo(base, c.Server.ClientKey)
}

func relTo(base, p string) string {
	p = strings.TrimSpace(p)
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

func (c *Config) applyEnv() {
	if c.LLM.APIKey == "" && c.LLM.APIKeyEnv != "" {
		c.LLM.APIKey = os.Getenv(c.LLM.APIKeyEnv)
	}
	if c.Server.Password == "" && c.Server.PasswordEnv != "" {
		c.Server.Password = os.Getenv(c.Server.PasswordEnv)
	}
	if c.Server.SASLAccount != "" && c.Server.SASLPassword == "" && c.Server.SASLPasswordEnv != "" {
		c.Server.SASLPassword = os.Getenv(c.Server.SASLPasswordEnv)
	}
	for name, ch := range c.Channel {
		if ch.Key == "" && ch.KeyEnv != "" {
			ch.Key = os.Getenv(ch.KeyEnv)
			c.Channel[name] = ch
		}
	}
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.Nick) == "" {
		return fmt.Errorf("nick is required")
	}
	if strings.TrimSpace(c.Server.Host) == "" {
		return fmt.Errorf("server.host is required")
	}
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port is invalid")
	}
	hasSASLAccount := strings.TrimSpace(c.Server.SASLAccount) != ""
	hasSASLPassword := c.Server.SASLPassword != ""
	if hasSASLAccount != hasSASLPassword {
		return fmt.Errorf("server SASL account and password must both be configured")
	}
	hasIRCSecret := c.Server.Password != "" || hasSASLAccount
	if hasIRCSecret && !c.Server.TLS {
		return fmt.Errorf("IRC passwords require server.tls = true")
	}
	if hasIRCSecret && c.Server.InsecureSkipVerify {
		return fmt.Errorf("IRC passwords cannot be used with insecure_skip_verify")
	}
	if c.Partyline.Listen != "" && !loopbackListen(c.Partyline.Listen) {
		return fmt.Errorf("plaintext partyline.listen must use a loopback address")
	}
	if c.Partyline.TLSListen != "" && (c.Partyline.TLSCert == "" || c.Partyline.TLSKey == "") {
		return fmt.Errorf("partyline TLS listen requires tls_cert and tls_key")
	}
	if c.Log.DebugIRC {
		return fmt.Errorf("log.debug_irc is disabled because raw IRC logs expose credentials")
	}
	if c.LLM.Enabled && c.LLM.APIKey != "" {
		endpoint, err := url.Parse(c.LLM.BaseURL)
		if err != nil || endpoint.Hostname() == "" {
			return fmt.Errorf("llm.base_url is invalid")
		}
		if endpoint.Scheme != "https" && !loopbackHost(endpoint.Hostname()) {
			return fmt.Errorf("LLM API keys require an HTTPS base_url (HTTP is allowed only on loopback)")
		}
	}
	if c.Server.DialTimeoutSec <= 0 {
		c.Server.DialTimeoutSec = 15
	}
	if c.Learn.HelloPerHour <= 0 {
		c.Learn.HelloPerHour = 10
	}
	if c.Username == "" {
		c.Username = c.Nick
	}
	if c.Store.Path == "" {
		c.Store.Path = "eggbot.db"
	}
	if c.Store.RetentionDays < 0 {
		c.Store.RetentionDays = 0
	}
	if c.Store.ChanlogDays < 0 {
		c.Store.ChanlogDays = 0
	}
	if c.Learn.ProductionLock {
		c.Learn.Hello = false
	}
	if c.LLM.Limits.TimeoutSec <= 0 {
		c.LLM.Limits.TimeoutSec = 60
	}
	if c.LLM.Limits.MaxToolSteps <= 0 {
		c.LLM.Limits.MaxToolSteps = 4
	}
	if c.LLM.Limits.HistoryLines <= 0 {
		c.LLM.Limits.HistoryLines = 40
	}
	if c.LLM.Limits.MaxOutputChars <= 0 {
		c.LLM.Limits.MaxOutputChars = 800
	}
	if c.LLM.Limits.MaxChannelChars <= 0 {
		c.LLM.Limits.MaxChannelChars = 350
	}
	if c.LLM.Limits.MaxDMChars <= 0 {
		c.LLM.Limits.MaxDMChars = 1200
	}
	if c.LLM.Limits.PerUserPerMin <= 0 {
		c.LLM.Limits.PerUserPerMin = 1
	}
	if c.LLM.Limits.OpPerMin <= 0 {
		c.LLM.Limits.OpPerMin = 4
	}
	if c.Channel == nil {
		c.Channel = map[string]Channel{}
	}
	for _, mask := range c.Owners.AutoOwnerHosts {
		nick, rest, ok := strings.Cut(mask, "!")
		user, host, hostOK := strings.Cut(rest, "@")
		if !ok || !hostOK || strings.ContainsAny(nick, "*?") || !c.IsOwnerHandle(nick) {
			return fmt.Errorf("owners.auto_owner_hosts mask %q must name a configured owner exactly", mask)
		}
		if wildcardOnly(user) && wildcardOnly(host) {
			return fmt.Errorf("owners.auto_owner_hosts mask %q trusts only a nickname; pin an ident or host", mask)
		}
	}
	for _, r := range strings.ToLower(c.Learn.HelloFlags) {
		switch r {
		case 'n', 'm', 'o':
			return fmt.Errorf("learn.hello_flags cannot grant administrative flags")
		}
	}
	return nil
}

func wildcardOnly(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || strings.Trim(s, "*?") == ""
}

func loopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Config) ServerAddr() string {
	host := strings.TrimSpace(c.Server.Host)
	host = strings.Trim(host, "[]")
	return net.JoinHostPort(host, fmt.Sprintf("%d", c.Server.Port))
}

func (c *Config) ServerEndpoints() []string {
	if len(c.Server.Endpoints) > 0 {
		out := make([]string, 0, len(c.Server.Endpoints))
		for _, e := range c.Server.Endpoints {
			e = strings.TrimSpace(e)
			if e != "" {
				out = append(out, e)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []string{c.ServerAddr()}
}

func (c *Config) NetworkID() string {
	if s := strings.TrimSpace(c.Server.Network); s != "" {
		return s
	}
	return c.Server.Host
}

func (c *Config) LLMTimeout() time.Duration {
	return time.Duration(c.LLM.Limits.TimeoutSec) * time.Second
}

func (c *Config) IsOwnerHandle(h string) bool {
	for _, o := range c.Owners.Handles {
		if strings.EqualFold(o, h) {
			return true
		}
	}
	return false
}

func (c *Config) ChannelNames() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, n := range c.Channels.Join {
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	for n := range c.Channel {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}
