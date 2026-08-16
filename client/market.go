// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package client

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/ChloePike/go-polymarket/types"
)

// normalizeTick renders the tick size (number or string) as the canonical
// map key form ("0.01", "0.1", ...).
func normalizeTick(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", t), "0"), ".")
	default:
		return fmt.Sprintf("%v", v)
	}
}

// GetTickSize returns the market tick size for a token, e.g. "0.01".
func (c *Client) GetTickSize(ctx context.Context, tokenID string) (string, error) {
	var out struct {
		MinimumTickSize any `json:"minimum_tick_size"`
	}
	q := url.Values{"token_id": {tokenID}}
	if err := c.get(ctx, types.EPTickSize, q, &out); err != nil {
		return "", err
	}
	return normalizeTick(out.MinimumTickSize), nil
}

// GetNegRisk reports whether a token trades on the neg-risk exchange.
func (c *Client) GetNegRisk(ctx context.Context, tokenID string) (bool, error) {
	var out struct {
		NegRisk bool `json:"neg_risk"`
	}
	q := url.Values{"token_id": {tokenID}}
	if err := c.get(ctx, types.EPNegRisk, q, &out); err != nil {
		return false, err
	}
	return out.NegRisk, nil
}

// GetBuilderFeeRates returns the configured maker/taker fee for a builder code.
func (c *Client) GetBuilderFeeRates(ctx context.Context, builderCode string) (types.BuilderFeeRates, error) {
	var out types.BuilderFeeRates
	err := c.get(ctx, types.EPBuilderFees+builderCode, nil, &out)
	return out, err
}

// GetBuilderTrades returns a page of trades attributed to a builder code.
func (c *Client) GetBuilderTrades(ctx context.Context, builderCode, cursor string) (types.BuilderTradesResponse, error) {
	var out types.BuilderTradesResponse
	q := url.Values{"builder_code": {builderCode}}
	if cursor != "" {
		q.Set("next_cursor", cursor)
	}
	err := c.get(ctx, types.EPBuilderTrades, q, &out)
	return out, err
}
