// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

// Package client is the Polymarket CLOB V2 REST client.
//
// Read endpoints (book, tick-size, neg-risk, builder fees/trades) need no
// credentials and work today. Trading (auth.go, orders.go) depends on the M1
// signing work in the sign package.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/ChloePike/go-polymarket/sign"
	"github.com/ChloePike/go-polymarket/types"
)

// Client talks to a CLOB host.
type Client struct {
	Host    string
	ChainID int64
	HTTP    *http.Client

	// Signer and Creds are optional; required only for trading.
	Signer sign.Signer
	Creds  *types.APICreds
}

// New returns a read-only client against the default mainnet host.
func New() *Client {
	return &Client{
		Host:    types.DefaultHost,
		ChainID: types.ChainPolygon,
		HTTP:    &http.Client{Timeout: 20 * time.Second},
	}
}

// get performs an unauthenticated GET and decodes JSON into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, nil, out)
}

// do is the shared request path; headers may be nil.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body []byte, headers map[string]string, out any) error {
	u := c.Host + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("go-polymarket: %s %s -> %d: %s", method, path, resp.StatusCode, string(data))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
