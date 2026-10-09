package gh

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
)

// ErrorKind groups GitHub failures by what the user can do about them.
type ErrorKind int

const (
	ErrOther ErrorKind = iota
	ErrAuth
	ErrNotFound
	ErrPermission
	ErrValidation
	ErrRateLimited
	ErrOffline
	// ErrGone means the resource was deleted for good, e.g. a deleted issue.
	ErrGone
)

// Error is a classified GitHub API failure.
type Error struct {
	Kind    ErrorKind
	Status  int
	Message string
	Repo    string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("GitHub API error (HTTP %d)", e.Status)
}

func kindOf(err error) ErrorKind {
	var ghErr *Error
	if errors.As(err, &ghErr) {
		return ghErr.Kind
	}
	return ErrOther
}

// IsOffline reports whether err means GitHub could not be reached, so the
// request is worth retrying later unchanged.
func IsOffline(err error) bool {
	kind := kindOf(err)
	return kind == ErrOffline || kind == ErrRateLimited
}

func IsNotFound(err error) bool { return kindOf(err) == ErrNotFound || kindOf(err) == ErrGone }

// IsGone reports whether err means the resource was deleted.
func IsGone(err error) bool { return kindOf(err) == ErrGone }

func IsValidation(err error) bool { return kindOf(err) == ErrValidation }

// UserMessage turns err into a short sentence suitable for a status bar.
func UserMessage(err error) string {
	if err == nil {
		return ""
	}
	var ghErr *Error
	if !errors.As(err, &ghErr) {
		return err.Error()
	}
	switch ghErr.Kind {
	case ErrAuth:
		return "GitHub authentication required. Run `gh auth login`."
	case ErrPermission:
		if ghErr.Repo != "" {
			return fmt.Sprintf("You don't have permission to do that in %s.", ghErr.Repo)
		}
		return "GitHub denied this action. Check your repository permissions."
	case ErrGone:
		return "That issue was deleted on GitHub."
	case ErrNotFound:
		if ghErr.Repo != "" {
			return fmt.Sprintf("Not found or not accessible: %s.", ghErr.Repo)
		}
		return "GitHub resource not found or not accessible."
	case ErrRateLimited:
		return "GitHub rate limit reached. triage will retry later."
	case ErrOffline:
		return "Can't reach GitHub. Changes are queued and will be sent when you're back online."
	case ErrValidation:
		if ghErr.Message != "" {
			return "GitHub rejected the change: " + ghErr.Message
		}
		return "GitHub rejected the change."
	default:
		return ghErr.Error()
	}
}

func classifyTransportError(err error, repo string) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	var netErr net.Error
	var urlErr *url.Error
	if errors.As(err, &netErr) || errors.As(err, &urlErr) || errors.Is(err, context.DeadlineExceeded) {
		return &Error{Kind: ErrOffline, Message: err.Error(), Repo: repo}
	}
	return err
}

func classifyStatus(status int, message string, rateRemaining string, repo string) *Error {
	e := &Error{Status: status, Message: message, Repo: repo}
	switch {
	case status == 401:
		e.Kind = ErrAuth
	case status == 403 && rateRemaining == "0", status == 429:
		e.Kind = ErrRateLimited
	case status == 403:
		e.Kind = ErrPermission
	case status == 404:
		e.Kind = ErrNotFound
	case status == 410:
		e.Kind = ErrGone
	case status == 422:
		e.Kind = ErrValidation
	case status >= 500:
		e.Kind = ErrOffline
	default:
		e.Kind = ErrOther
	}
	return e
}
