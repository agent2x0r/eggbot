package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// ProviderError is an HTTP failure from the LLM provider.
type ProviderError struct {
	Status int
	Body   string
}

func (e *ProviderError) Error() string {
	if e == nil {
		return ""
	}
	switch e.Status {
	case 401, 403:
		return "llm unauthorized"
	case 429:
		return "llm rate-limited"
	default:
		if e.Body != "" {
			return fmt.Sprintf("llm http %d: %s", e.Status, e.Body)
		}
		return fmt.Sprintf("llm http %d", e.Status)
	}
}

func providerStatus(status int, raw []byte) error {
	if status < 400 {
		return nil
	}
	return &ProviderError{Status: status, Body: trim(raw, 200)}
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "deadline exceeded") || strings.Contains(s, "timeout")
}

func friendlyErr(err error) error {
	if err == nil {
		return nil
	}
	if isTimeout(err) {
		return fmt.Errorf("search timed out — try again, or ask a shorter question")
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		if pe.Status == 429 {
			return fmt.Errorf("AI is busy, try in a minute")
		}
		return fmt.Errorf("couldn't look that up right now")
	}
	s := err.Error()
	if strings.Contains(s, "rate-limited") || strings.Contains(s, "429") {
		return fmt.Errorf("AI is busy, try in a minute")
	}
	return fmt.Errorf("couldn't look that up right now")
}
