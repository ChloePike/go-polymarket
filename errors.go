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

	// ErrNotSent wraps every failure that happens before a request leaves
	// this process. It is what lets a caller tell "the exchange refused
	// this" and "this never got there" apart from "nobody knows", which is
	// the distinction that decides whether resubmitting an order is safe.
	// See Indeterminate.
	ErrNotSent = errors.New("polymarket: request was not sent")
)

// Indeterminate reports whether err leaves it unknown what the exchange did.
//
// This is the question to ask after a write fails. A request that was never
// sent, and a request the exchange answered with a 4xx, both definitely had
// no effect: the order does not exist and building a new one is safe. A
// connection that dropped, a timeout, or a 5xx is different — the exchange
// may have received the order, acted on it, and failed to say so.
//
//	if _, err := c.PostOrder(ctx, order, polymarket.GTC, opts); err != nil {
//		if !polymarket.Indeterminate(err) {
//			return err // refused or never sent; nothing exists to reconcile
//		}
//		// Its fate is unknown. Find out; do not resubmit.
//		reconcile(ctx, order)
//	}
//
// Unrecognised errors are reported as indeterminate. Treating an unknown
// failure as harmless is how an account ends up holding a position twice.
func Indeterminate(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotSent) || errors.Is(err, ErrNoSigner) || errors.Is(err, ErrNoCredentials) {
		return false
	}
	var apiErr *Error
	if errors.As(err, &apiErr) {
		// The exchange answered. A 4xx is a refusal and nothing was
		// created; a 5xx may have been a failure to report a success.
		return apiErr.StatusCode >= 500
	}
	return true
}

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
