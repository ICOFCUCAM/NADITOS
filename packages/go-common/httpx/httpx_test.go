package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

// TestWriteJSON_StatusAndContentType: sets Content-Type and writes
// body before status code is locked in. Every handler in the
// platform writes through this; a regression in headers breaks every
// API consumer.
func TestWriteJSON_StatusAndContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.WriteJSON(rec, http.StatusCreated, map[string]string{"id": "abc"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type: %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"id":"abc"`) {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestWriteErr_KnownError: a typed *Error is rendered with its
// status, code, and message (the contract the front-end relies on).
func TestWriteErr_KnownError(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.WriteErr(rec, httpx.Err(http.StatusConflict, "duplicate", "row exists"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"code":"duplicate"`) ||
		!strings.Contains(body, `"message":"row exists"`) {
		t.Fatalf("body: %s", body)
	}
}

// TestWriteErr_PlainError_HidesDetail: a generic non-typed error
// renders 500 with a generic message — internal details don't leak
// when DebugErrors is off (the production default).
func TestWriteErr_PlainError_HidesDetail(t *testing.T) {
	prev := httpx.DebugErrors
	defer func() { httpx.DebugErrors = prev }()
	httpx.DebugErrors = false

	rec := httptest.NewRecorder()
	httpx.WriteErr(rec, errPlain("password=hunter2 leaked into stack"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("internal detail leaked: %s", rec.Body.String())
	}
}

// TestWriteErr_DebugErrors_ShowsDetail: with DebugErrors flipped on
// (test fixtures), the underlying error message is exposed so devs
// can debug.
func TestWriteErr_DebugErrors_ShowsDetail(t *testing.T) {
	prev := httpx.DebugErrors
	defer func() { httpx.DebugErrors = prev }()
	httpx.DebugErrors = true

	rec := httptest.NewRecorder()
	httpx.WriteErr(rec, errPlain("specific debug detail"))
	if !strings.Contains(rec.Body.String(), "specific debug detail") {
		t.Fatalf("debug mode should expose detail: %s", rec.Body.String())
	}
}

// TestReadJSON_Strict_DisallowUnknownFields: extra fields in the
// payload are rejected. This is what stops officers from sneaking
// "amount":"1.00" into a fine-issue body and overriding the
// server-priced amount.
func TestReadJSON_Strict_DisallowUnknownFields(t *testing.T) {
	type known struct {
		Plate string `json:"plate"`
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(
		`{"plate":"AB-12-CD","amount":"1.00"}`))
	var dst known
	err := httpx.ReadJSON(r, &dst)
	if err == nil {
		t.Fatal("expected unknown-field rejection")
	}
}

// TestReadJSON_AcceptsKnownFields: a strict-but-clean payload parses.
func TestReadJSON_AcceptsKnownFields(t *testing.T) {
	type known struct {
		Plate string `json:"plate"`
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"plate":"AB-12-CD"}`))
	var dst known
	if err := httpx.ReadJSON(r, &dst); err != nil {
		t.Fatal(err)
	}
	if dst.Plate != "AB-12-CD" {
		t.Fatalf("plate: %s", dst.Plate)
	}
}

type errPlain string

func (e errPlain) Error() string { return string(e) }
