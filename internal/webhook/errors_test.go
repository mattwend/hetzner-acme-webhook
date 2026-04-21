// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package webhook

import (
	"errors"
	"fmt"
	"testing"
)

func TestStabilizeHTTPError(t *testing.T) {
	tests := []struct {
		status   int
		wantCode APIErrorCode
	}{
		{401, ErrCodeUnauthorized},
		{403, ErrCodeForbidden},
		{404, ErrCodeNotFound},
		{409, ErrCodeConflict},
		{429, ErrCodeRateLimited},
		{500, ErrCodeServer},
		{502, ErrCodeServer},
		{418, ErrCodeUnknown},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.status), func(t *testing.T) {
			raw := &httpError{StatusCode: tt.status, Body: "test"}
			stable := stabilizeHTTPError(raw)
			var apiErr *APIError
			if !errors.As(stable, &apiErr) {
				t.Fatalf("expected *APIError, got %T", stable)
			}
			if apiErr.Code != tt.wantCode {
				t.Fatalf("got code %q, want %q", apiErr.Code, tt.wantCode)
			}
			if apiErr.StatusCode != tt.status {
				t.Fatalf("got status %d, want %d", apiErr.StatusCode, tt.status)
			}
		})
	}
}

func TestStabilizeNonHTTPError(t *testing.T) {
	orig := errors.New("not an http error")
	got := stabilizeHTTPError(orig)
	if got != orig {
		t.Fatalf("expected original error, got %v", got)
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(&APIError{Code: ErrCodeNotFound, StatusCode: 404}) {
		t.Fatal("expected true for APIError not_found")
	}
	if !IsNotFound(&httpError{StatusCode: 404}) {
		t.Fatal("expected true for httpError 404")
	}
	if IsNotFound(&httpError{StatusCode: 500}) {
		t.Fatal("expected false for httpError 500")
	}
	if IsNotFound(errors.New("other")) {
		t.Fatal("expected false for generic error")
	}
}

func TestAPIErrorMessage(t *testing.T) {
	err := &APIError{Code: ErrCodeUnauthorized, StatusCode: 401, Message: "bad token"}
	got := err.Error()
	want := "hetzner api unauthorized (http 401): bad token"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
