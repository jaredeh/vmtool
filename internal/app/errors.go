package app

import (
	"errors"

	"github.com/jaredeh/vmtool/pkg/vmtool"
)

type Kind int

const (
	KindInvalid Kind = iota
	KindNotFound
	KindConflict
	KindBadGateway
	KindInternal
)

// Error is the shared workflow error. CLI/TUI print Error() and Output;
// REST maps Kind to HTTP status.
type Error struct {
	Kind   Kind
	Op     string
	Err    error
	Output string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		if e.Op != "" {
			return e.Op + ": " + e.Err.Error()
		}
		return e.Err.Error()
	}
	if e.Op != "" {
		return e.Op + " failed"
	}
	return "error"
}

func (e *Error) Unwrap() error { return e.Err }

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	var ae *Error
	if AsError(err, &ae) {
		if ae.Op == "" {
			ae.Op = op
		}
		return ae
	}
	k := KindInternal
	if vmtool.IsNotFound(err) {
		k = KindNotFound
	}
	return &Error{Kind: k, Op: op, Err: err}
}

func AsError(err error, target **Error) bool {
	if err == nil {
		return false
	}
	var ae *Error
	if !errors.As(err, &ae) {
		return false
	}
	*target = ae
	return true
}

func invalid(op string, err error) *Error {
	return &Error{Kind: KindInvalid, Op: op, Err: err}
}

func conflict(op string, err error) *Error {
	return &Error{Kind: KindConflict, Op: op, Err: err}
}

func badGateway(op string, err error) *Error {
	return &Error{Kind: KindBadGateway, Op: op, Err: err}
}

func internal(op string, err error) *Error {
	return &Error{Kind: KindInternal, Op: op, Err: err}
}
