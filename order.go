// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/ChloePike/go-polymarket/internal/amount"
	"github.com/ChloePike/go-polymarket/internal/eip712"
)

// Side is the direction of an order. It travels as a string on the wire and as
// a uint8 in the signed struct.
type Side string

const (
	Buy  Side = "BUY"
	Sell Side = "SELL"
)

func (s Side) uint8() uint8 {
	if s == Sell {
		return 1
	}
	return 0
}

// SignatureType selects how the exchange verifies an order's signature.
type SignatureType uint8

const (
	// SigEOA is a plain externally-owned account signing for itself.
	SigEOA SignatureType = 0
	// SigPolyProxy is an EOA signing for its Polymarket proxy wallet.
	SigPolyProxy SignatureType = 1
	// SigPolyGnosisSafe is an EOA signing for its Polymarket Gnosis Safe.
	SigPolyGnosisSafe SignatureType = 2
	// SigEIP1271 is a contract wallet verifying through EIP-1271.
	SigEIP1271 SignatureType = 3
)

// OrderType is an order's time in force.
type OrderType string

const (
	GTC OrderType = "GTC" // rest on the book until cancelled
	GTD OrderType = "GTD" // rest until the expiration timestamp
	FOK OrderType = "FOK" // fill completely at once or not at all
	FAK OrderType = "FAK" // fill what is available now, cancel the rest
)

// Exchange versions. An order is signed against exactly one of them; the
// version picks both the EIP-712 domain version and the verifying contract.
const (
	V1 = 1
	V2 = 2
	V3 = 3
)

// A UserOrder is a limit order as a caller expresses it: a price and a number
// of shares. Everything else has a default.
type UserOrder struct {
	TokenID string // ERC-1155 id of the outcome being traded
	Price   string // decimal price in (0,1), such as "0.52"
	Size    string // number of shares
	Side    Side

	// BuilderCode attributes the fill to a builder for fee sharing. It is a
	// bytes32 hex string; empty means no attribution.
	BuilderCode string

	// Expiration is a unix-seconds deadline for a GTD order. It rides on the
	// wire but is NOT part of the signature.
	Expiration int64

	// Taker restricts who may fill the order. Empty means anyone.
	Taker string

	// Metadata is a bytes32 field the exchange passes through. Empty means zero.
	Metadata string
}

// A MarketOrder is an order sized by how much the caller wants to spend or
// sell rather than by a share count at a price.
type MarketOrder struct {
	TokenID string
	// Amount is USDC for a buy and shares for a sell.
	Amount string
	Side   Side
	// Price is the marketable price the amounts are computed against, usually
	// derived from the book. See Client.MarketPrice.
	Price string

	BuilderCode string
	Expiration  int64
	Taker       string
	Metadata    string
}

// OrderOptions carries the market and account facts an order needs beyond its
// price, size and side.
type OrderOptions struct {
	// TickSize is the market's minimum price increment, such as "0.01". It
	// selects the rounding limits, so it must match the market. Client.
	// CreateOrder fills it in when it is empty.
	TickSize string

	// NegRisk selects the neg-risk exchange contract. It must match the
	// market: signing against the wrong contract yields a rejected order.
	NegRisk bool

	// Version is the exchange version. Zero means V2.
	Version int

	// SignatureType is the verification path. The zero value, SigEOA, signs
	// with the key itself.
	SignatureType SignatureType

	// Funder is the address holding the funds when it differs from the signing
	// key, as with a proxy or Safe wallet. Empty means the signer.
	Funder string

	// Salt fixes the order salt instead of drawing a random one. Tests use it;
	// production should leave it zero.
	Salt int64

	// Timestamp fixes the order timestamp in unix milliseconds. Zero uses the
	// current time.
	Timestamp int64
}

func (o OrderOptions) version() int {
	if o.Version == 0 {
		return V2
	}
	return o.Version
}

// An Order is a fully resolved order, ready to sign. Eleven of its fields are
// covered by the signature; Taker and Expiration ride along on the wire and
// are deliberately excluded. See orderTypeString.
type Order struct {
	Salt          string
	Maker         string
	Signer        string
	Taker         string // wire only, not signed
	TokenID       string
	MakerAmount   string
	TakerAmount   string
	Side          Side
	SignatureType SignatureType
	Timestamp     string
	Expiration    string // wire only, not signed
	Metadata      string
	Builder       string
}

// A SignedOrder is an Order with its 65-byte signature as 0x-prefixed hex.
type SignedOrder struct {
	Order
	Signature string
}

// BuildOrder resolves a UserOrder into a signable Order. signerAddress is the
// address that will sign; the maker is the funder when one is set, otherwise
// the signer.
func BuildOrder(u UserOrder, signerAddress string, opts OrderOptions) (Order, error) {
	cfg, ok := amount.ByTickSize[opts.TickSize]
	if !ok {
		return Order{}, fmt.Errorf("polymarket: unknown tick size %q", opts.TickSize)
	}
	price, err := amount.ParseDecimal(u.Price)
	if err != nil {
		return Order{}, err
	}
	size, err := amount.ParseDecimal(u.Size)
	if err != nil {
		return Order{}, err
	}
	if err := checkPrice(price, opts.TickSize); err != nil {
		return Order{}, err
	}
	raw := amount.Limit(u.Side == Buy, size, price, cfg)
	return assemble(u.TokenID, u.Side, raw, signerAddress, opts,
		u.BuilderCode, u.Metadata, u.Taker, u.Expiration)
}

// BuildMarketOrder resolves a MarketOrder into a signable Order. A market buy
// spends Amount in USDC; a market sell offers Amount in shares.
func BuildMarketOrder(m MarketOrder, signerAddress string, opts OrderOptions) (Order, error) {
	cfg, ok := amount.ByTickSize[opts.TickSize]
	if !ok {
		return Order{}, fmt.Errorf("polymarket: unknown tick size %q", opts.TickSize)
	}
	price, err := amount.ParseDecimal(m.Price)
	if err != nil {
		return Order{}, err
	}
	amt, err := amount.ParseDecimal(m.Amount)
	if err != nil {
		return Order{}, err
	}
	raw, err := amount.Market(m.Side == Buy, amt, price, cfg)
	if err != nil {
		return Order{}, err
	}
	return assemble(m.TokenID, m.Side, raw, signerAddress, opts,
		m.BuilderCode, m.Metadata, m.Taker, m.Expiration)
}

func assemble(tokenID string, side Side, raw amount.Raw, signerAddress string, opts OrderOptions,
	builderCode, metadata, taker string, expiration int64) (Order, error) {

	if tokenID == "" {
		return Order{}, fmt.Errorf("polymarket: order has no token id")
	}
	if _, ok := new(big.Int).SetString(tokenID, 10); !ok {
		return Order{}, fmt.Errorf("polymarket: token id %q is not a decimal integer", tokenID)
	}

	// Validate the pass-through fields here rather than letting them fail at
	// digest time. A builder code that is not a bytes32 is a mistyped or
	// truncated code, and the caller should learn that while holding the
	// string they typed — not later, from an Order that looked well formed.
	builder := orDefault(builderCode, ZeroBytes32)
	if _, err := eip712.Bytes32(builder); err != nil {
		return Order{}, fmt.Errorf("polymarket: builder code: %w", err)
	}
	meta := orDefault(metadata, ZeroBytes32)
	if _, err := eip712.Bytes32(meta); err != nil {
		return Order{}, fmt.Errorf("polymarket: order metadata: %w", err)
	}
	takerAddress := orDefault(taker, ZeroAddress)
	if _, err := eip712.Address(takerAddress); err != nil {
		return Order{}, fmt.Errorf("polymarket: order taker: %w", err)
	}

	maker := opts.Funder
	if maker == "" {
		maker = signerAddress
	}
	if _, err := eip712.Address(maker); err != nil {
		return Order{}, fmt.Errorf("polymarket: order maker: %w", err)
	}
	// An EIP-1271 wallet verifies against the funder, so the signed signer
	// field names the wallet rather than the key that produced the bytes.
	signer := signerAddress
	if opts.SignatureType == SigEIP1271 {
		signer = maker
	}
	if _, err := eip712.Address(signer); err != nil {
		return Order{}, fmt.Errorf("polymarket: order signer: %w", err)
	}

	salt := opts.Salt
	if salt == 0 {
		var err error
		if salt, err = randomSalt(); err != nil {
			return Order{}, err
		}
	}
	ts := opts.Timestamp
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}

	return Order{
		Salt:          strconv.FormatInt(salt, 10),
		Maker:         maker,
		Signer:        signer,
		Taker:         takerAddress,
		TokenID:       tokenID,
		MakerAmount:   amount.Fixed(raw.Maker),
		TakerAmount:   amount.Fixed(raw.Taker),
		Side:          side,
		SignatureType: opts.SignatureType,
		Timestamp:     strconv.FormatInt(ts, 10),
		Expiration:    strconv.FormatInt(expiration, 10),
		Metadata:      meta,
		Builder:       builder,
	}, nil
}

// OrderDigest returns the 32 bytes an order's signature covers.
func OrderDigest(o Order, chainID int64, opts OrderOptions) ([32]byte, error) {
	domain, err := orderDomain(chainID, opts.version(), opts.NegRisk)
	if err != nil {
		return [32]byte{}, err
	}
	separator, err := domain.Separator()
	if err != nil {
		return [32]byte{}, err
	}

	// Field order follows orderTypeString exactly. Taker and Expiration are
	// absent on purpose: they ride on the wire but are not signed.
	var enc eip712.Encoder
	enc.Uint("salt", o.Salt)
	enc.Address("maker", o.Maker)
	enc.Address("signer", o.Signer)
	enc.Uint("tokenId", o.TokenID)
	enc.Uint("makerAmount", o.MakerAmount)
	enc.Uint("takerAmount", o.TakerAmount)
	enc.Uint8("side", o.Side.uint8())
	enc.Uint8("signatureType", uint8(o.SignatureType))
	enc.Uint("timestamp", o.Timestamp)
	enc.Bytes32("metadata", o.Metadata)
	enc.Bytes32("builder", o.Builder)

	structHash, err := enc.StructHash(eip712.TypeHash(orderTypeString))
	if err != nil {
		return [32]byte{}, fmt.Errorf("polymarket: order: %w", err)
	}
	return eip712.Digest(separator, structHash), nil
}

// SignOrder signs an order for a chain and returns it with its signature.
func SignOrder(o Order, chainID int64, opts OrderOptions, s Signer) (SignedOrder, error) {
	if s == nil {
		return SignedOrder{}, fmt.Errorf("polymarket: SignOrder needs a Signer")
	}
	digest, err := OrderDigest(o, chainID, opts)
	if err != nil {
		return SignedOrder{}, err
	}
	sig, err := s.SignDigest(digest)
	if err != nil {
		return SignedOrder{}, fmt.Errorf("polymarket: signing order: %w", err)
	}
	return SignedOrder{Order: o, Signature: "0x" + hex.EncodeToString(sig)}, nil
}

// orderDomain resolves the EIP-712 domain for an exchange version. V1 and V2
// each have a normal and a neg-risk contract; V3 has one.
func orderDomain(chainID int64, version int, negRisk bool) (eip712.Domain, error) {
	c, ok := ContractsFor(chainID)
	if !ok {
		return eip712.Domain{}, fmt.Errorf("polymarket: unknown chain id %d", chainID)
	}
	var contract, domainVersion string
	switch version {
	case V1:
		domainVersion = exchangeV1Version
		contract = c.Exchange
		if negRisk {
			contract = c.NegRiskExchange
		}
	case V2:
		domainVersion = exchangeV2Version
		contract = c.ExchangeV2
		if negRisk {
			contract = c.NegRiskExchangeV2
		}
	case V3:
		domainVersion = exchangeV3Version
		contract = c.ExchangeV3
	default:
		return eip712.Domain{}, fmt.Errorf("polymarket: unsupported exchange version %d", version)
	}
	return eip712.Domain{
		Name:              exchangeDomainName,
		Version:           domainVersion,
		ChainID:           big.NewInt(chainID),
		VerifyingContract: contract,
	}, nil
}

// checkPrice rejects a price outside the tradable range, which the exchange
// would reject anyway but only after the order has been signed and sent.
func checkPrice(price *big.Rat, tickSize string) error {
	tick, err := amount.ParseDecimal(tickSize)
	if err != nil {
		return err
	}
	upper := new(big.Rat).Sub(big.NewRat(1, 1), tick)
	if price.Cmp(tick) < 0 || price.Cmp(upper) > 0 {
		return fmt.Errorf("polymarket: price %s is outside [%s, %s] for tick size %s",
			price.FloatString(6), tick.FloatString(6), upper.FloatString(6), tickSize)
	}
	return nil
}

// randomSalt draws a salt below 2^52.
//
// The bound is not arbitrary. The order salt is signed as a uint256 but the
// POST /order body carries it as a JSON *number*, and a JSON number is a
// float64 to most parsers. Above 2^53 the value a parser reconstructs differs
// from the value that was signed, and the exchange then verifies a different
// struct and rejects the order — intermittently, only for large salts.
func randomSalt() (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1<<52))
	if err != nil {
		return 0, fmt.Errorf("polymarket: generating salt: %w", err)
	}
	// Zero means "draw one" to the caller, so never hand back zero.
	return n.Int64() + 1, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
