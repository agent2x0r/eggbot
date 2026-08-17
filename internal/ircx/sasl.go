package ircx

import (
	"strings"

	"github.com/ergochat/irc-go/ircutils"
)

func saslPLAIN(account, password string) []string {
	raw := []byte("\x00" + account + "\x00" + password)
	return ircutils.EncodeSASLResponse(raw)
}

func saslEXTERNAL() []string {
	return ircutils.EncodeSASLResponse(nil)
}

func pickSASLMech(offered []string, haveCert bool, havePlain bool) string {
	want := make([]string, 0, 2)
	if haveCert {
		want = append(want, "EXTERNAL")
	}
	if havePlain {
		want = append(want, "PLAIN")
	}
	if len(offered) == 0 {
		if len(want) > 0 {
			return want[0]
		}
		return ""
	}
	have := map[string]struct{}{}
	for _, m := range offered {
		have[strings.ToUpper(strings.TrimSpace(m))] = struct{}{}
	}
	for _, m := range want {
		if _, ok := have[m]; ok {
			return m
		}
	}
	return ""
}
