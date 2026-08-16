// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Errors reported before a request is made.
var (
	// ErrNoSigner is returned when an operation needs a wallet key and the
	// session has none.
	ErrNoSigner = errors.New("polymarket: operation needs a Signer")

	// ErrNoCredentials is returned when an operation needs level-2
	// credentials. Obtain them with the clob package's key handshake, or
	// supply them with WithCredentials.
	ErrNoCredentials = errors.New("polymarket: operation needs API credentials")
)

// An Error reports a request the API refused. Inspect StatusCode to tell a
// bad request from a rate limit or an outage:
//
//	var apiErr *polymarket.Error
//	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests {
//		...
//	}
type Error struct {
	// Method and URL identify the request that failed. URL is the path, not
	// the full address, so an error is safe to log without leaking a host
	// or a query.
	Method string
	URL    string

	// StatusCode is the HTTP status the API returned.
	StatusCode int

	// Message is the error text the API supplied, when it supplied one.
	Message string

	// Body is the raw response, truncated for legibility.
	Body string
}

func (e *Error) Error() string {
	detail := e.Message
	if detail == "" {
		detail = e.Body
	}
	return fmt.Sprintf("polymarket: %s %s: %d %s: %s",
		e.Method, e.URL, e.StatusCode, http.StatusText(e.StatusCode), detail)
}

// errorBody covers the shapes the API uses to report a failure. Different
// endpoints pick different field names for the same thing.
type errorBody struct {
	Error    string `json:"error"`
	ErrorMsg string `json:"errorMsg"`
	Message  string `json:"message"`
	Detail   string `json:"detail"`
}

// errorMessage pulls a human-readable message out of an error body. It is
// best effort: an unparseable body simply yields no message.
func errorMessage(data []byte) string {
	var body errorBody
	if err := json.Unmarshal(data, &body); err != nil {
		return ""
	}
	for _, s := range []string{body.Error, body.ErrorMsg, body.Message, body.Detail} {
		if s != "" {
			return s
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
