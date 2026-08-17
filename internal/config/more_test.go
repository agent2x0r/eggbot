package config

import "testing"

func TestServerEndpointsAndChannelNames(t *testing.T) {
	cfg := Defaults()
	cfg.Server.Host = "irc.example.net"
	cfg.Server.Port = 6697
	if len(cfg.ServerEndpoints()) != 1 {
		t.Fatal(cfg.ServerEndpoints())
	}
	cfg.Server.Endpoints = []string{"", " irc.a:6697 ", "irc.b:6697"}
	if got := cfg.ServerEndpoints(); len(got) != 2 {
		t.Fatal(got)
	}
	cfg.Server.Endpoints = []string{"", " "}
	if got := cfg.ServerEndpoints(); len(got) != 1 {
		t.Fatal(got)
	}
	cfg.Server.Network = "EFNet"
	if cfg.NetworkID() != "EFNet" {
		t.Fatal(cfg.NetworkID())
	}
	cfg.Channels.Join = []string{"#a", "", "#a", "#b"}
	cfg.Channel = map[string]Channel{"#b": {}, "#c": {}}
	names := cfg.ChannelNames()
	if len(names) != 3 {
		t.Fatal(names)
	}
	if loopbackListen("bad") || !loopbackListen("localhost:3333") || !loopbackListen("127.0.0.1:1") {
		t.Fatal("listen")
	}
	if loopbackHost("example.net") || !loopbackHost("::1") {
		t.Fatal("host")
	}
}

func TestApplyEnv(t *testing.T) {
	t.Setenv("XAI_API_KEY", "k")
	t.Setenv("EGGBOT_SERVER_PASSWORD", "pw")
	t.Setenv("EGGBOT_SASL_PASSWORD", "sasl")
	t.Setenv("EGGBOT_CHAN_KEY", "secret")
	cfg := Defaults()
	cfg.LLM.APIKey = ""
	cfg.Server.Password = ""
	cfg.Server.PasswordEnv = "EGGBOT_SERVER_PASSWORD"
	cfg.Server.SASLAccount = "eggbot"
	cfg.Server.SASLPassword = ""
	cfg.Server.SASLPasswordEnv = "EGGBOT_SASL_PASSWORD"
	cfg.Channel = map[string]Channel{"#c": {KeyEnv: "EGGBOT_CHAN_KEY"}}
	cfg.applyEnv()
	if cfg.LLM.APIKey != "k" || cfg.Server.Password != "pw" || cfg.Server.SASLPassword != "sasl" || cfg.Channel["#c"].Key != "secret" {
		t.Fatalf("%+v %+v", cfg.LLM, cfg.Server)
	}
}
