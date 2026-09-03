package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// ErrStale means a newer ask from this nick replaced this one; do not speak the reply.
var ErrStale = errors.New("stale ask")

// RateLimitError is a per-user or per-channel ask cap. RetryAt is when a slot opens.
type RateLimitError struct {
	Scope   string // "user" or "channel"
	Limit   int
	RetryAt time.Time
	msg     string
}

func (e *RateLimitError) Error() string {
	if e == nil {
		return ""
	}
	return e.msg
}

func IsRateLimited(err error) (*RateLimitError, bool) {
	var r *RateLimitError
	if errors.As(err, &r) {
		return r, true
	}
	return nil, false
}

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
		return fmt.Errorf("that took too long — try again, or ask a shorter question")
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
