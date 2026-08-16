// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ChloePike/go-polymarket/sign"
	"github.com/ChloePike/go-polymarket/types"
)

// CreateOrDeriveAPIKey performs the L1 handshake to obtain L2 credentials.
// Requires c.Signer. Depends on sign.BuildClobAuthSignature (M1).
func (c *Client) CreateOrDeriveAPIKey(ctx context.Context) (types.APICreds, error) {
	if c.Signer == nil {
		return types.APICreds{}, fmt.Errorf("go-polymarket: CreateOrDeriveAPIKey needs a Signer")
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	l1sig, err := sign.BuildClobAuthSignature(c.Signer, c.ChainID, ts, 0)
	if err != nil {
		return types.APICreds{}, err
	}
	headers := sign.L1Headers{
		Address:   c.Signer.Address(),
		Signature: l1sig,
		Timestamp: ts,
		Nonce:     "0",
	}.Map()

	// Try create (POST); on empty key fall back to derive (GET).
	var creds types.APICreds
	if err := c.do(ctx, http.MethodPost, types.EPCreateAPIKey, nil, nil, headers, &creds); err != nil {
		return types.APICreds{}, err
	}
	if creds.Key == "" {
		if err := c.do(ctx, http.MethodGet, types.EPDeriveAPIKey, nil, nil, headers, &creds); err != nil {
			return types.APICreds{}, err
		}
	}
	c.Creds = &creds
	return creds, nil
}

// PostOrder signs (L2 HMAC) and submits a signed order.
// Requires c.Signer and c.Creds. The HMAC is computed over the exact JSON body.
func (c *Client) PostOrder(ctx context.Context, so types.SignedOrder, orderType types.OrderType) (types.PostOrderResponse, error) {
	if c.Signer == nil || c.Creds == nil {
		return types.PostOrderResponse{}, fmt.Errorf("go-polymarket: PostOrder needs Signer and Creds")
	}
	salt, err := strconv.ParseInt(so.Salt, 10, 64)
	if err != nil {
		return types.PostOrderResponse{}, fmt.Errorf("go-polymarket: bad salt: %w", err)
	}
	reqBody := types.PostOrderRequest{
		DeferExec: false,
		PostOnly:  false,
		Order: types.WireOrder{
			Salt:          salt,
			Maker:         so.Maker,
			Signer:        so.Signer,
			Taker:         so.Taker,
			TokenID:       so.TokenID,
			MakerAmount:   so.MakerAmount,
			TakerAmount:   so.TakerAmount,
			Side:          so.Side,
			SignatureType: so.SignatureType,
			Timestamp:     so.Timestamp,
			Expiration:    so.Expiration,
			Metadata:      so.Metadata,
			Builder:       so.Builder,
			Signature:     so.Signature,
		},
		Owner:     c.Creds.Key,
		OrderType: orderType,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return types.PostOrderResponse{}, err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	l2, err := sign.BuildL2Headers(*c.Creds, c.Signer.Address(), ts, http.MethodPost, types.EPPostOrder, string(body))
	if err != nil {
		return types.PostOrderResponse{}, err
	}
	var out types.PostOrderResponse
	err = c.do(ctx, http.MethodPost, types.EPPostOrder, nil, body, l2.Map(), &out)
	return out, err
}
