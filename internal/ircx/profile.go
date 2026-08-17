package ircx

// Profile is the machine-readable IRCv3 contract eggbot implements.
type Profile struct {
	CAPVersion      int      `json:"cap_version"`
	Preferred       []string `json:"preferred"`
	Disabled        []string `json:"disabled"`
	RequiredSASL    bool     `json:"required_sasl_when_configured"`
	EchoMessage     bool     `json:"echo_message"`
	DraftExtensions bool     `json:"draft_extensions"`
}

func ProductionProfile() Profile {
	disabled := make([]string, 0, len(DisabledCaps))
	for name := range DisabledCaps {
		disabled = append(disabled, name)
	}
	return Profile{
		CAPVersion:      302,
		Preferred:       append([]string{}, DefaultPreferredCaps...),
		Disabled:        disabled,
		RequiredSASL:    true,
		EchoMessage:     false,
		DraftExtensions: false,
	}
}
