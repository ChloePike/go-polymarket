// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package order

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/ChloePike/go-polymarket/types"
)

// MarketMeta is the per-market data needed to build an order. Fetch via the
// client's read endpoints (tick-size, neg-risk).
type MarketMeta struct {
	TickSize string // e.g. "0.01"
	NegRisk  bool
}

// Build turns a UserOrder into an unsigned Order for a maker/signer EOA.
//
// nowMillis and salt are injectable for deterministic golden tests; pass zero
// to use time.Now and a crypto-random salt.
func Build(u types.UserOrder, maker string, meta MarketMeta, nowMillis, salt int64) (types.Order, error) {
	cfg, ok := types.RoundingByTickSize[meta.TickSize]
	if !ok {
		return types.Order{}, fmt.Errorf("go-polymarket/order: unknown tick size %q", meta.TickSize)
	}

	price, err := ratFromDecimal(u.Price)
	if err != nil {
		return types.Order{}, err
	}
	size, err := ratFromDecimal(u.Size)
	if err != nil {
		return types.Order{}, err
	}

	raw := GetRawAmounts(u.Side, size, price, cfg)
	makerAmt, takerAmt := FixedAmounts(raw)

	if nowMillis == 0 {
		nowMillis = time.Now().UnixMilli()
	}
	if salt == 0 {
		salt, err = randomSalt()
		if err != nil {
			return types.Order{}, err
		}
	}

	builder := u.BuilderCode
	if builder == "" {
		builder = types.Bytes32Zero
	}
	expiration := u.Expiration
	if expiration == "" {
		expiration = "0"
	}

	return types.Order{
		Salt:          fmt.Sprintf("%d", salt),
		Maker:         maker,
		Signer:        maker, // EOA: signer == maker
		Taker:         "0x0000000000000000000000000000000000000000",
		TokenID:       u.TokenID,
		MakerAmount:   makerAmt,
		TakerAmount:   takerAmt,
		Side:          u.Side,
		SignatureType: types.SigEOA,
		Timestamp:     fmt.Sprintf("%d", nowMillis),
		Expiration:    expiration,
		Metadata:      types.Bytes32Zero,
		Builder:       builder,
	}, nil
}

func randomSalt() (int64, error) {
	// A random positive int64; the protocol only needs uniqueness.
	max := new(big.Int).Lsh(big.NewInt(1), 62)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return 0, err
	}
	return n.Int64(), nil
}
