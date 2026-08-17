package ircx

import "strings"

// Disposition is how eggbot treats a client-facing RFC command or numeric.
type Disposition string

const (
	Handled       Disposition = "handled"
	Surfaced      Disposition = "surfaced"
	Ignored       Disposition = "ignored"
	NotApplicable Disposition = "not-applicable"
)

type CommandClass struct {
	Name        string
	Disposition Disposition
	Notes       string
}

// RFCClientCommands is the finite client-facing RFC 1459/2812 inventory.
// Server-to-server and operator-only commands are NotApplicable.
var RFCClientCommands = []CommandClass{
	{Name: "PASS", Disposition: Handled, Notes: "sent during registration when configured"},
	{Name: "NICK", Disposition: Handled, Notes: "registration, 433 fallback, inbound nick changes"},
	{Name: "USER", Disposition: Handled, Notes: "registration"},
	{Name: "OPER", Disposition: NotApplicable, Notes: "eggbot does not become an IRC operator"},
	{Name: "MODE", Disposition: Handled, Notes: "user and channel modes via ISUPPORT parser"},
	{Name: "SERVICE", Disposition: NotApplicable, Notes: "service registration is not a bot role"},
	{Name: "QUIT", Disposition: Handled, Notes: "inbound member removal; outbound on shutdown"},
	{Name: "SQUIT", Disposition: NotApplicable, Notes: "server-to-server"},
	{Name: "JOIN", Disposition: Handled},
	{Name: "PART", Disposition: Handled},
	{Name: "TOPIC", Disposition: Handled},
	{Name: "NAMES", Disposition: Handled, Notes: "353/366 synchronization"},
	{Name: "LIST", Disposition: Surfaced},
	{Name: "INVITE", Disposition: Handled},
	{Name: "KICK", Disposition: Handled},
	{Name: "PRIVMSG", Disposition: Handled},
	{Name: "NOTICE", Disposition: Handled},
	{Name: "MOTD", Disposition: Ignored, Notes: "375/372/376 consumed for registration completeness"},
	{Name: "LUSERS", Disposition: Ignored},
	{Name: "VERSION", Disposition: Surfaced},
	{Name: "STATS", Disposition: Surfaced},
	{Name: "LINKS", Disposition: Surfaced},
	{Name: "TIME", Disposition: Surfaced},
	{Name: "CONNECT", Disposition: NotApplicable, Notes: "operator"},
	{Name: "TRACE", Disposition: Surfaced},
	{Name: "ADMIN", Disposition: Surfaced},
	{Name: "INFO", Disposition: Surfaced},
	{Name: "SERVLIST", Disposition: Surfaced},
	{Name: "SQUERY", Disposition: Surfaced},
	{Name: "WHO", Disposition: Handled, Notes: "352/354/315 identity hydration"},
	{Name: "WHOIS", Disposition: Surfaced},
	{Name: "WHOWAS", Disposition: Surfaced},
	{Name: "KILL", Disposition: Surfaced, Notes: "treated as QUIT of the victim when seen"},
	{Name: "PING", Disposition: Handled, Notes: "automatic protocol reply"},
	{Name: "PONG", Disposition: Handled, Notes: "keepalive"},
	{Name: "ERROR", Disposition: Handled, Notes: "disconnects the session"},
	{Name: "AWAY", Disposition: Handled, Notes: "away-notify when enabled"},
	{Name: "REHASH", Disposition: NotApplicable, Notes: "server operator"},
	{Name: "DIE", Disposition: NotApplicable, Notes: "server operator"},
	{Name: "RESTART", Disposition: NotApplicable, Notes: "server operator"},
	{Name: "SUMMON", Disposition: NotApplicable, Notes: "obsolete client feature"},
	{Name: "USERS", Disposition: NotApplicable, Notes: "obsolete client feature"},
	{Name: "WALLOPS", Disposition: Surfaced},
	{Name: "USERHOST", Disposition: Surfaced},
	{Name: "ISON", Disposition: Surfaced},
}

var RFCClientNumerics = []CommandClass{
	{Name: "001", Disposition: Handled, Notes: "welcome; registration success"},
	{Name: "002", Disposition: Ignored},
	{Name: "003", Disposition: Ignored},
	{Name: "004", Disposition: Surfaced, Notes: "server version snapshot"},
	{Name: "005", Disposition: Handled, Notes: "ISUPPORT"},
	{Name: "200", Disposition: Surfaced},
	{Name: "251", Disposition: Ignored},
	{Name: "301", Disposition: Handled, Notes: "away reply"},
	{Name: "305", Disposition: Handled, Notes: "unaway"},
	{Name: "306", Disposition: Handled, Notes: "now away"},
	{Name: "311", Disposition: Surfaced},
	{Name: "315", Disposition: Handled, Notes: "end of WHO"},
	{Name: "322", Disposition: Surfaced},
	{Name: "324", Disposition: Handled, Notes: "channel mode snapshot"},
	{Name: "329", Disposition: Surfaced, Notes: "channel creation time"},
	{Name: "331", Disposition: Handled, Notes: "no topic"},
	{Name: "332", Disposition: Handled, Notes: "topic"},
	{Name: "333", Disposition: Handled, Notes: "topic metadata"},
	{Name: "341", Disposition: Surfaced},
	{Name: "352", Disposition: Handled, Notes: "WHO reply"},
	{Name: "353", Disposition: Handled, Notes: "NAMES"},
	{Name: "354", Disposition: Handled, Notes: "WHOX"},
	{Name: "366", Disposition: Handled, Notes: "end of NAMES"},
	{Name: "367", Disposition: Handled, Notes: "ban list"},
	{Name: "368", Disposition: Handled, Notes: "end of ban list"},
	{Name: "372", Disposition: Ignored},
	{Name: "375", Disposition: Ignored},
	{Name: "376", Disposition: Handled, Notes: "end of MOTD; ISUPPORT complete"},
	{Name: "401", Disposition: Surfaced},
	{Name: "403", Disposition: Surfaced},
	{Name: "404", Disposition: Surfaced},
	{Name: "405", Disposition: Surfaced},
	{Name: "412", Disposition: Surfaced, Notes: "no text to send"},
	{Name: "421", Disposition: Handled, Notes: "unknown command; used to complete missing CAP"},
	{Name: "432", Disposition: Handled, Notes: "erroneous nickname"},
	{Name: "433", Disposition: Handled, Notes: "nickname in use; try alt nick"},
	{Name: "436", Disposition: Handled, Notes: "nick collision"},
	{Name: "437", Disposition: Handled},
	{Name: "441", Disposition: Surfaced},
	{Name: "442", Disposition: Surfaced},
	{Name: "443", Disposition: Surfaced},
	{Name: "451", Disposition: Handled, Notes: "not registered"},
	{Name: "461", Disposition: Surfaced},
	{Name: "462", Disposition: Surfaced},
	{Name: "464", Disposition: Handled, Notes: "password mismatch; fatal"},
	{Name: "465", Disposition: Handled, Notes: "banned from server; fatal"},
	{Name: "471", Disposition: Surfaced},
	{Name: "473", Disposition: Surfaced},
	{Name: "474", Disposition: Surfaced},
	{Name: "475", Disposition: Surfaced},
	{Name: "482", Disposition: Surfaced},
	{Name: "900", Disposition: Handled, Notes: "SASL logged in"},
	{Name: "901", Disposition: Handled, Notes: "SASL logged out"},
	{Name: "902", Disposition: Handled, Notes: "SASL nick locked"},
	{Name: "903", Disposition: Handled, Notes: "SASL success"},
	{Name: "904", Disposition: Handled, Notes: "SASL fail"},
	{Name: "905", Disposition: Handled, Notes: "SASL too long"},
	{Name: "906", Disposition: Handled, Notes: "SASL aborted"},
	{Name: "907", Disposition: Handled, Notes: "SASL already authenticated"},
	{Name: "908", Disposition: Handled, Notes: "SASL mechanisms"},
	{Name: "200", Disposition: Surfaced, Notes: "RPL_TRACELINK"},
	{Name: "201", Disposition: Surfaced},
	{Name: "202", Disposition: Surfaced},
	{Name: "203", Disposition: Surfaced},
	{Name: "204", Disposition: Surfaced},
	{Name: "205", Disposition: Surfaced},
	{Name: "206", Disposition: Surfaced},
	{Name: "207", Disposition: Surfaced},
	{Name: "208", Disposition: Surfaced},
	{Name: "209", Disposition: Surfaced},
	{Name: "211", Disposition: Surfaced, Notes: "RPL_STATSLINKINFO"},
	{Name: "212", Disposition: Surfaced},
	{Name: "219", Disposition: Surfaced, Notes: "RPL_ENDOFSTATS"},
	{Name: "221", Disposition: Handled, Notes: "RPL_UMODEIS"},
	{Name: "242", Disposition: Surfaced},
	{Name: "243", Disposition: Surfaced},
	{Name: "251", Disposition: Ignored, Notes: "LUSERS"},
	{Name: "252", Disposition: Ignored},
	{Name: "253", Disposition: Ignored},
	{Name: "254", Disposition: Ignored},
	{Name: "255", Disposition: Ignored},
	{Name: "256", Disposition: Surfaced, Notes: "ADMIN"},
	{Name: "257", Disposition: Surfaced},
	{Name: "258", Disposition: Surfaced},
	{Name: "259", Disposition: Surfaced},
	{Name: "263", Disposition: Surfaced, Notes: "RPL_TRYAGAIN"},
	{Name: "265", Disposition: Ignored},
	{Name: "266", Disposition: Ignored},
	{Name: "276", Disposition: Handled, Notes: "whois certfp"},
	{Name: "300", Disposition: Ignored},
	{Name: "302", Disposition: Surfaced, Notes: "USERHOST"},
	{Name: "303", Disposition: Surfaced, Notes: "ISON"},
	{Name: "307", Disposition: Surfaced},
	{Name: "312", Disposition: Surfaced},
	{Name: "313", Disposition: Surfaced},
	{Name: "314", Disposition: Surfaced, Notes: "WHOWAS"},
	{Name: "317", Disposition: Surfaced},
	{Name: "318", Disposition: Surfaced},
	{Name: "319", Disposition: Surfaced},
	{Name: "321", Disposition: Surfaced, Notes: "LIST start"},
	{Name: "323", Disposition: Surfaced},
	{Name: "325", Disposition: Surfaced},
	{Name: "346", Disposition: Surfaced, Notes: "invite list"},
	{Name: "347", Disposition: Surfaced},
	{Name: "348", Disposition: Surfaced, Notes: "except list"},
	{Name: "349", Disposition: Surfaced},
	{Name: "351", Disposition: Surfaced, Notes: "VERSION"},
	{Name: "364", Disposition: Surfaced, Notes: "LINKS"},
	{Name: "365", Disposition: Surfaced},
	{Name: "369", Disposition: Surfaced},
	{Name: "371", Disposition: Ignored, Notes: "INFO"},
	{Name: "374", Disposition: Ignored},
	{Name: "381", Disposition: NotApplicable, Notes: "you are oper"},
	{Name: "382", Disposition: NotApplicable},
	{Name: "391", Disposition: Surfaced, Notes: "TIME"},
	{Name: "392", Disposition: NotApplicable},
	{Name: "393", Disposition: NotApplicable},
	{Name: "394", Disposition: NotApplicable},
	{Name: "395", Disposition: NotApplicable},
	{Name: "402", Disposition: Surfaced},
	{Name: "406", Disposition: Surfaced},
	{Name: "407", Disposition: Surfaced},
	{Name: "409", Disposition: Surfaced},
	{Name: "411", Disposition: Surfaced},
	{Name: "413", Disposition: Surfaced},
	{Name: "414", Disposition: Surfaced},
	{Name: "415", Disposition: Surfaced},
	{Name: "422", Disposition: Handled, Notes: "no MOTD; registration still completes"},
	{Name: "423", Disposition: Surfaced},
	{Name: "424", Disposition: Surfaced},
	{Name: "431", Disposition: Handled, Notes: "no nickname given"},
	{Name: "444", Disposition: NotApplicable},
	{Name: "445", Disposition: NotApplicable},
	{Name: "446", Disposition: NotApplicable},
	{Name: "467", Disposition: Surfaced},
	{Name: "472", Disposition: Surfaced},
	{Name: "476", Disposition: Surfaced},
	{Name: "477", Disposition: Surfaced},
	{Name: "478", Disposition: Surfaced},
	{Name: "481", Disposition: NotApplicable, Notes: "oper-only"},
	{Name: "483", Disposition: Surfaced},
	{Name: "484", Disposition: Surfaced},
	{Name: "485", Disposition: Surfaced},
	{Name: "491", Disposition: NotApplicable},
	{Name: "501", Disposition: Surfaced},
	{Name: "502", Disposition: Surfaced},
}

func Classify(command string) CommandClass {
	command = strings.ToUpper(command)
	for _, c := range RFCClientCommands {
		if c.Name == command {
			return c
		}
	}
	for _, c := range RFCClientNumerics {
		if c.Name == command {
			return c
		}
	}
	switch command {
	case "CAP", "AUTHENTICATE", "ACCOUNT", "CHGHOST", "SETNAME", "TAGMSG", "BATCH",
		"FAIL", "WARN", "NOTE", "ACK":
		return CommandClass{Name: command, Disposition: Handled, Notes: "IRCv3"}
	}
	return CommandClass{Name: command, Disposition: Surfaced, Notes: "unknown; preserved as a raw event"}
}

func MustClassifyAll() []CommandClass {
	out := append([]CommandClass{}, RFCClientCommands...)
	out = append(out, RFCClientNumerics...)
	return out
}
