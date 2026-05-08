// Package httpx contains small HTTP helpers used across services.
package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
)

type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func Err(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

var (
	ErrUnauthorized = Err(http.StatusUnauthorized, "unauthorized", "authentication required")
	ErrForbidden    = Err(http.StatusForbidden, "forbidden", "permission denied")
	ErrNotFound     = Err(http.StatusNotFound, "not_found", "resource not found")
	ErrConflict     = Err(http.StatusConflict, "conflict", "duplicate resource")
	ErrBadRequest   = Err(http.StatusBadRequest, "bad_request", "invalid request")
)

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteErr(w http.ResponseWriter, err error) {
	var e *Error
	if errors.As(err, &e) {
		WriteJSON(w, e.Status, e)
		return
	}
	WriteJSON(w, http.StatusInternalServerError, &Error{
		Code: "internal", Message: "internal error",
	})
}

func ReadJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return Err(http.StatusBadRequest, "bad_json", err.Error())
	}
	return nil
}
