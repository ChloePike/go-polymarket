// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import "encoding/json"

// Event is implemented by every message a Conn can deliver through Read.
// See the package doc for why this is a sealed interface rather than a
// map[string]any or a single flattened struct.
type Event interface {
	event()
}

// eventTypeEnvelope extracts just a frame's event_type discriminator, so
// decode can choose which concrete Event type to unmarshal the rest of the
// frame into.
type eventTypeEnvelope struct {
	EventType string `json:"event_type"`
}

// PriceLevel is one price/size pair on one side of an order book. Both
// fields are decimal strings, matching the wire format, to avoid
// floating-point precision loss.
type PriceLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// BookEvent is a full order-book snapshot for one CLOB token, delivered on
// the market channel as event_type "book" -- once per subscribed asset
// immediately after subscribing (when initial_dump is enabled, the
// default), and again for every asset after each automatic reconnect.
//
// The live wire format is a strict superset of Polymarket's published
// AsyncAPI schema for this event: TickSize and LastTradePrice are present
// on every snapshot observed live but are not declared by the formal
// spec, which requires only EventType, AssetID, Market, Bids, Asks,
// Timestamp, and Hash.
//
// Bids and Asks were observed sorted worst-price-first, best-price-last on
// both sides (bids ascending toward the spread, asks descending toward
// it). Polymarket does not document this as a guaranteed ordering; treat
// it as observed behavior, not a contract.
//
// Hash is a content hash of this book for detecting local drift, but it is
// computed over a different, REST-shaped representation than this struct:
// see Book and BookHash.
type BookEvent struct {
	EventType      string       `json:"event_type"`
	AssetID        string       `json:"asset_id"`
	Market         string       `json:"market"`
	Bids           []PriceLevel `json:"bids"`
	Asks           []PriceLevel `json:"asks"`
	TickSize       string       `json:"tick_size"`
	LastTradePrice string       `json:"last_trade_price"`
	Timestamp      string       `json:"timestamp"`
	Hash           string       `json:"hash"`
}

func (BookEvent) event() {}

// PriceChange is one entry in a PriceChangeEvent. Size "0" means the price
// level was removed.
type PriceChange struct {
	AssetID string `json:"asset_id"`
	Price   string `json:"price"`
	Size    string `json:"size"`
	// Side is "BUY" or "SELL".
	Side    string `json:"side"`
	Hash    string `json:"hash"`
	BestBid string `json:"best_bid"`
	BestAsk string `json:"best_ask"`
}

// PriceChangeEvent is an incremental order-book update, event_type
// "price_change". A single frame can, and live routinely does, carry
// changes for every token of a market at once: both outcomes of a binary
// market update in lockstep and are reported together in one
// PriceChanges slice.
type PriceChangeEvent struct {
	EventType    string        `json:"event_type"`
	Market       string        `json:"market"`
	PriceChanges []PriceChange `json:"price_changes"`
	Timestamp    string        `json:"timestamp"`
}

func (PriceChangeEvent) event() {}

// LastTradePriceEvent fires on trade execution, event_type
// "last_trade_price". Documented only: not observed live in this
// package's verification window (see package doc), which happened to
// capture only book and price_change frames.
type LastTradePriceEvent struct {
	EventType  string `json:"event_type"`
	AssetID    string `json:"asset_id"`
	Market     string `json:"market"`
	Price      string `json:"price"`
	Size       string `json:"size"`
	FeeRateBps string `json:"fee_rate_bps"`
	// Side is from the taker's perspective: "BUY" or "SELL".
	Side            string `json:"side"`
	Timestamp       string `json:"timestamp"`
	TransactionHash string `json:"transaction_hash"`
}

func (LastTradePriceEvent) event() {}

// TickSizeChangeEvent reports a market's tick size changing, typically as
// its price approaches 0 or 1, event_type "tick_size_change". Documented
// only: not observed live.
type TickSizeChangeEvent struct {
	EventType   string `json:"event_type"`
	AssetID     string `json:"asset_id"`
	Market      string `json:"market"`
	OldTickSize string `json:"old_tick_size"`
	NewTickSize string `json:"new_tick_size"`
	Timestamp   string `json:"timestamp"`
}

func (TickSizeChangeEvent) event() {}

// BestBidAskEvent reports the current best bid, best ask, and spread for
// one asset, event_type "best_bid_ask". Only sent when the subscribe
// request set CustomFeatureEnabled (see WithCustomFeatureEnabled).
// Documented only: the feature flag was not exercised during this
// package's live verification.
type BestBidAskEvent struct {
	EventType string `json:"event_type"`
	Market    string `json:"market"`
	AssetID   string `json:"asset_id"`
	BestBid   string `json:"best_bid"`
	BestAsk   string `json:"best_ask"`
	Spread    string `json:"spread"`
	Timestamp string `json:"timestamp"`
}

func (BestBidAskEvent) event() {}

// EventMessage is the parent-event metadata attached to NewMarketEvent and
// MarketResolvedEvent when the market belongs to a grouped event (e.g. a
// multi-outcome event made of several binary markets).
type EventMessage struct {
	ID          string `json:"id"`
	Ticker      string `json:"ticker"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// NewMarketEvent reports a newly created market/token, event_type
// "new_market". Only sent when the subscribe request set
// CustomFeatureEnabled. Documented only: not observed live, so it is
// unknown which of the fields the docs mark optional actually appear in
// practice; absent fields simply decode to their Go zero value.
type NewMarketEvent struct {
	EventType             string        `json:"event_type"`
	ID                    string        `json:"id"`
	Question              string        `json:"question"`
	Market                string        `json:"market"`
	Slug                  string        `json:"slug"`
	Description           string        `json:"description"`
	AssetIDs              []string      `json:"assets_ids"`
	Outcomes              []string      `json:"outcomes"`
	EventMessage          *EventMessage `json:"event_message"`
	Timestamp             string        `json:"timestamp"`
	Tags                  []string      `json:"tags"`
	ConditionID           string        `json:"condition_id"`
	Active                bool          `json:"active"`
	ClobTokenIDs          []string      `json:"clob_token_ids"`
	SportsMarketType      string        `json:"sports_market_type"`
	Line                  string        `json:"line"`
	GameStartTime         string        `json:"game_start_time"`
	OrderPriceMinTickSize string        `json:"order_price_min_tick_size"`
	GroupItemTitle        string        `json:"group_item_title"`
}

func (NewMarketEvent) event() {}

// MarketResolvedEvent reports a market's resolution, event_type
// "market_resolved". Only sent when the subscribe request set
// CustomFeatureEnabled. Documented only: not observed live.
type MarketResolvedEvent struct {
	EventType      string        `json:"event_type"`
	ID             string        `json:"id"`
	Market         string        `json:"market"`
	AssetIDs       []string      `json:"assets_ids"`
	WinningAssetID string        `json:"winning_asset_id"`
	WinningOutcome string        `json:"winning_outcome"`
	EventMessage   *EventMessage `json:"event_message"`
	Timestamp      string        `json:"timestamp"`
	Tags           []string      `json:"tags"`
}

func (MarketResolvedEvent) event() {}

// OrderEvent reports an order lifecycle change on the user channel,
// event_type "order". Documented only: the user channel requires real L2
// API credentials, out of scope for this package's live verification (see
// package doc).
type OrderEvent struct {
	EventType string `json:"event_type"`
	ID        string `json:"id"`
	Owner     string `json:"owner"`
	Market    string `json:"market"`
	AssetID   string `json:"asset_id"`
	// Side is "BUY" or "SELL".
	Side            string   `json:"side"`
	OrderOwner      string   `json:"order_owner"`
	OriginalSize    string   `json:"original_size"`
	SizeMatched     string   `json:"size_matched"`
	Price           string   `json:"price"`
	AssociateTrades []string `json:"associate_trades"`
	Outcome         string   `json:"outcome"`
	// Type is "PLACEMENT", "UPDATE", or "CANCELLATION".
	Type       string `json:"type"`
	CreatedAt  string `json:"created_at"`
	Expiration string `json:"expiration"`
	// OrderType is "GTC", "GTD", or "FOK".
	OrderType string `json:"order_type"`
	// Status is documented as at least "LIVE", "MATCHED", "CANCELED", "and
	// others" -- not an exhaustive enum.
	Status       string `json:"status"`
	MakerAddress string `json:"maker_address"`
	Timestamp    string `json:"timestamp"`
}

func (OrderEvent) event() {}

// MakerOrder is one resting order matched by a TradeEvent's taker order.
type MakerOrder struct {
	OrderID       string `json:"order_id"`
	Owner         string `json:"owner"`
	MakerAddress  string `json:"maker_address"`
	MatchedAmount string `json:"matched_amount"`
	Price         string `json:"price"`
	FeeRateBps    string `json:"fee_rate_bps"`
	AssetID       string `json:"asset_id"`
	Outcome       string `json:"outcome"`
	Side          string `json:"side"`
}

// TradeEvent reports a trade/fill on the user channel, event_type "trade".
// Documented only: see OrderEvent.
//
// Status follows the state machine Polymarket documents for trades:
// matched (internal, never broadcast) -> mined -> confirmed (terminal
// success), or retrying -> failed (terminal failure).
type TradeEvent struct {
	EventType string `json:"event_type"`
	// Type is the literal "TRADE".
	Type         string `json:"type"`
	ID           string `json:"id"`
	TakerOrderID string `json:"taker_order_id"`
	Market       string `json:"market"`
	AssetID      string `json:"asset_id"`
	// Side is "BUY" or "SELL", from the taker's perspective.
	Side       string `json:"side"`
	Size       string `json:"size"`
	Price      string `json:"price"`
	FeeRateBps string `json:"fee_rate_bps"`
	// Status is "MATCHED", "MINED", "CONFIRMED", "RETRYING", or "FAILED".
	Status          string       `json:"status"`
	MatchTime       string       `json:"match_time"`
	LastUpdate      string       `json:"last_update"`
	Outcome         string       `json:"outcome"`
	Owner           string       `json:"owner"`
	TradeOwner      string       `json:"trade_owner"`
	MakerAddress    string       `json:"maker_address"`
	TransactionHash string       `json:"transaction_hash"`
	BucketIndex     int          `json:"bucket_index"`
	MakerOrders     []MakerOrder `json:"maker_orders"`
	// TraderSide is "TAKER" or "MAKER".
	TraderSide string `json:"trader_side"`
	Timestamp  string `json:"timestamp"`
}

func (TradeEvent) event() {}

// ReconnectEvent is a synthetic event Read delivers whenever a Conn
// re-establishes its connection after a drop and resends its full
// subscription. It is not part of Polymarket's wire protocol; this
// package injects it into the stream so a caller can tell a resubscribe
// happened rather than silently losing coverage. See the package doc's
// Reconnection section for what to do next on each channel.
type ReconnectEvent struct {
	// Attempt is a 1-based count of reconnects this Conn has made since
	// Dial, incrementing by one on every ReconnectEvent delivered.
	Attempt uint64
	// Cause is the error that ended the previous connection.
	Cause error
}

func (ReconnectEvent) event() {}

// UnknownEvent is delivered when a frame's event_type (on the CLOB
// channels) or topic/type pair (on RTDS) is not one this package
// recognizes. Raw holds the undecoded JSON object so a caller can still
// inspect it. This is not a general-purpose escape hatch for every event
// type this package knows about -- each of those still has its own named,
// typed struct above -- it exists only because Polymarket's live wire
// format has proven to be a superset of its published schemas (see
// BookEvent), so treating a genuinely unrecognized shape as a fatal decode
// error would make this client more fragile than the server's own
// contract.
type UnknownEvent struct {
	EventType string
	Raw       json.RawMessage
}

func (UnknownEvent) event() {}
