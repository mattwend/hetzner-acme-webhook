// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package webhook

import (
	"errors"
	"fmt"
	"strings"
)

// APIErrorCode is a stable, machine-readable error code returned by the
// Hetzner Cloud API or derived from HTTP status codes.
type APIErrorCode string

const (
	ErrCodeUnauthorized APIErrorCode = "unauthorized"
	ErrCodeForbidden    APIErrorCode = "forbidden"
	ErrCodeNotFound     APIErrorCode = "not_found"
	ErrCodeConflict     APIErrorCode = "conflict"
	ErrCodeRateLimited  APIErrorCode = "rate_limited"
	ErrCodeServer       APIErrorCode = "server_error"
	ErrCodeUnknown      APIErrorCode = "unknown"
)

// APIError represents a stable, classifiable error from the Hetzner Cloud API.
// It wraps the raw HTTP error and adds a machine-readable code.
type APIError struct {
	Code       APIErrorCode
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("hetzner api %s (http %d): %s", e.Code, e.StatusCode, e.Message)
}

// stabilizeHTTPError converts an httpError into an APIError with a stable code.
func stabilizeHTTPError(err error) error {
	var httpErr *httpError
	if !errors.As(err, &httpErr) {
		return err
	}
	code := classifyStatus(httpErr.StatusCode)
	return &APIError{
		Code:       code,
		StatusCode: httpErr.StatusCode,
		Message:    httpErr.Body,
	}
}

func classifyStatus(status int) APIErrorCode {
	switch {
	case status == 401:
		return ErrCodeUnauthorized
	case status == 403:
		return ErrCodeForbidden
	case status == 404:
		return ErrCodeNotFound
	case status == 409:
		return ErrCodeConflict
	case status == 429:
		return ErrCodeRateLimited
	case status >= 500:
		return ErrCodeServer
	default:
		return ErrCodeUnknown
	}
}

// IsNotFound reports whether the error is an API "not found" error.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == ErrCodeNotFound
	}
	var httpErr *httpError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == 404
	}
	return false
}

// IsDuplicateTXTValue reports whether the error indicates the requested TXT
// value already exists in the rrset, which makes Present effectively already
// satisfied and safe to treat as success.
func IsDuplicateTXTValue(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 422 && strings.Contains(strings.ToLower(apiErr.Message), "duplicate value")
	}
	var httpErr *httpError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == 422 && strings.Contains(strings.ToLower(httpErr.Body), "duplicate value")
	}
	return false
}
