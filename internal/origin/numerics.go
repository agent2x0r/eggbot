package origin

// Parse353 handles both RFC2812 (4 params) and the short form
// (353 #chan :nicks) used by chonkline and some RFC1459 daemons.
func Parse353(params []string) (channel, names string, ok bool) {
	switch len(params) {
	case 2:
		if IsChannel(params[0]) {
			return params[0], params[1], true
		}
	case 3:
		if IsChannel(params[1]) {
			return params[1], params[2], true
		}
		if IsChannel(params[0]) {
			return params[0], params[2], true
		}
	default:
		if len(params) >= 4 && IsChannel(params[2]) {
			return params[2], params[3], true
		}
	}
	return "", "", false
}

// Parse332: "332 #chan :topic" or "332 nick #chan :topic".
func Parse332(params []string) (channel, topic string, ok bool) {
	switch len(params) {
	case 2:
		if IsChannel(params[0]) {
			return params[0], params[1], true
		}
	case 3:
		if IsChannel(params[1]) {
			return params[1], params[2], true
		}
		if IsChannel(params[0]) {
			return params[0], params[2], true
		}
	}
	return "", "", false
}

// Parse366: "366 #chan :End of names" or "366 nick #chan :..."
func Parse366(params []string) (channel string, ok bool) {
	for _, p := range params {
		if IsChannel(p) {
			return p, true
		}
	}
	return "", false
}
