package apperr

import (
	"errors"
	"net/http"
)

type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return http.StatusText(e.Code)
	}

	return e.Err.Error()
}

func (e *Error) Unwrap() error {
	return e.Err
}

func New(code int, err error) error {
	return &Error{Code: code, Err: err}
}

func StatusCode(err error) int {
	var appErr *Error
	if errors.As(err, &appErr) && appErr.Code > 0 {
		return appErr.Code
	}

	return http.StatusInternalServerError
}
