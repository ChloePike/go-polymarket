// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package relayer

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ChloePike/go-polymarket"
)

// The vectors in testdata/relayer-vectors.json are the output of
// Polymarket's own relayer client. They exist because nothing else can catch
// a mistake in this file: these signatures are checked inside a wallet
// contract, on chain, after the relayer has already paid to send them. There
// is no error message and no refund.
//
// The private key is the well-known Hardhat development account. It is public
// and holds nothing.
const hardhatKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

// relayerVectors is the whole vector file.
type relayerVectors struct {
	Source        string           `json:"source"`
	ChainID       int64            `json:"chainId"`
	Owner         string           `json:"owner"`
	Contracts     vectorContracts  `json:"contracts"`
	Safe          vectorSafe       `json:"safe"`
	Proxy         vectorProxy      `json:"proxy"`
	DepositWallet vectorDepositSet `json:"depositWallet"`
}

// vectorContracts are the addresses the vectors were generated against.
type vectorContracts struct {
	SafeFactory                 string `json:"SafeFactory"`
	SafeMultisend               string `json:"SafeMultisend"`
	ProxyFactory                string `json:"ProxyFactory"`
	RelayHub                    string `json:"RelayHub"`
	DepositWalletFactory        string `json:"DepositWalletFactory"`
	DepositWalletImplementation string `json:"DepositWalletImplementation"`
	Relay                       string `json:"relay"`
	Wallet                      string `json:"wallet"`
}

// vectorCall is one call as the Safe and proxy vectors express it.
type vectorCall struct {
	To        string `json:"to"`
	Value     string `json:"value"`
	Data      string `json:"data"`
	Operation int    `json:"operation"`
	TypeCode  string `json:"typeCode"`
}

// vectorBatchCall is one call as the deposit wallet expresses it.
type vectorBatchCall struct {
	Target string `json:"target"`
	Value  string `json:"value"`
	Data   string `json:"data"`
}

// vectorRequest is a submit request as the official client produced it.
type vectorRequest struct {
	Type            string               `json:"type"`
	From            string               `json:"from"`
	To              string               `json:"to"`
	ProxyWallet     string               `json:"proxyWallet"`
	Data            string               `json:"data"`
	Nonce           string               `json:"nonce"`
	Signature       string               `json:"signature"`
	SignatureParams SignatureParams      `json:"signatureParams"`
	Deposit         *DepositWalletParams `json:"depositWalletParams"`
}

// vectorSafeCase is one Safe transaction and the request it produced.
type vectorSafeCase struct {
	Nonce        string        `json:"nonce"`
	Transactions []vectorCall  `json:"transactions"`
	Request      vectorRequest `json:"request"`
}

// vectorSafe holds the Safe cases and the multisend call they aggregate to.
type vectorSafe struct {
	One       vectorSafeCase `json:"one"`
	Many      vectorSafeCase `json:"many"`
	Multisend vectorCall     `json:"multisend"`
}

// vectorProxyCase is one proxy transaction and the request it produced.
type vectorProxyCase struct {
	Nonce    string        `json:"nonce"`
	GasPrice string        `json:"gasPrice"`
	GasLimit string        `json:"gasLimit"`
	Relay    string        `json:"relay"`
	Calls    []vectorCall  `json:"calls"`
	Request  vectorRequest `json:"request"`
}

// vectorProxyCalldata is a call list and the calldata it encodes to.
type vectorProxyCalldata struct {
	Calls []vectorCall `json:"calls"`
	Data  string       `json:"data"`
}

// vectorProxy holds the proxy cases.
type vectorProxy struct {
	One         vectorProxyCase     `json:"one"`
	CalldataTwo vectorProxyCalldata `json:"calldataTwo"`
}

// vectorDepositCase is one deposit wallet batch and the request it produced.
type vectorDepositCase struct {
	Nonce    string            `json:"nonce"`
	Deadline string            `json:"deadline"`
	Calls    []vectorBatchCall `json:"calls"`
	Request  vectorRequest     `json:"request"`
}

// vectorDepositSet holds the batches: several calls, one, and none.
type vectorDepositSet struct {
	Many  vectorDepositCase `json:"many"`
	One   vectorDepositCase `json:"one"`
	Empty vectorDepositCase `json:"empty"`
}

func loadRelayerVectors(t *testing.T) relayerVectors {
	t.Helper()
	b, err := os.ReadFile("../testdata/relayer-vectors.json")
	if err != nil {
		t.Fatalf("relayer vectors: %v", err)
	}
	var v relayerVectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("relayer vectors: %v", err)
	}
	return v
}

// vectorContractsFor returns the addresses the vectors were generated with,
// so that a change to the repository's own contract table shows up as a
// derivation failure rather than as a silently different signature.
func (v relayerVectors) contracts() polymarket.Contracts {
	return polymarket.Contracts{
		SafeFactory:                 v.Contracts.SafeFactory,
		SafeMultisend:               v.Contracts.SafeMultisend,
		ProxyFactory:                v.Contracts.ProxyFactory,
		RelayHub:                    v.Contracts.RelayHub,
		DepositWalletFactory:        v.Contracts.DepositWalletFactory,
		DepositWalletImplementation: v.Contracts.DepositWalletImplementation,
	}
}

// safeCalls converts the vector's transactions into this package's calls.
func safeCalls(in []vectorCall) []Call {
	out := make([]Call, len(in))
	for i, c := range in {
		out[i] = Call{To: c.To, Value: c.Value, Data: c.Data, Operation: Operation(c.Operation)}
	}
	return out
}

// proxyCalls converts the vector's calls, whose type code counts from one.
func proxyCalls(in []vectorCall) []Call {
	out := make([]Call, len(in))
	for i, c := range in {
		op := OpCall
		if c.TypeCode == "2" {
			op = OpDelegateCall
		}
		out[i] = Call{To: c.To, Value: c.Value, Data: c.Data, Operation: op}
	}
	return out
}

// TestSafeTransactionMatchesTheOfficialClient covers the Gnosis Safe path,
// both for a single call and for a batch through the multisend contract. The
// batch is the interesting one: aggregating changes the target, the calldata
// and the operation code, and all three are signed.
func TestSafeTransactionMatchesTheOfficialClient(t *testing.T) {
	v := loadRelayerVectors(t)
	key, err := polymarket.NewPrivateKey(hardhatKey)
	if err != nil {
		t.Fatal(err)
	}
	contracts := v.contracts()

	cases := map[string]vectorSafeCase{"one call": v.Safe.One, "a batch": v.Safe.Many}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := BuildSafeTransaction(key, SafeParams{
				Owner:   v.Owner,
				Nonce:   tc.Nonce,
				Calls:   safeCalls(tc.Transactions),
				ChainID: v.ChainID,
			}, contracts)
			if err != nil {
				t.Fatal(err)
			}
			assertRequest(t, got, tc.Request)
			if got.SignatureParams.Operation != tc.Request.SignatureParams.Operation {
				t.Errorf("operation = %s, want %s",
					got.SignatureParams.Operation, tc.Request.SignatureParams.Operation)
			}
		})
	}

	// A Safe marks an eth_sign signature by raising v past 28. Left at 27 the
	// Safe recovers from the unprefixed digest and gets a stranger.
	sig := v.Safe.One.Request.Signature
	switch last := sig[len(sig)-2:]; last {
	case "1f", "20":
	default:
		t.Errorf("the vector's v byte is %q, want 1f or 20: the vectors no longer use eth_sign", last)
	}
}

// TestSafeMultisendEncoding pins the packed layout the multisend contract
// walks. It has no length prefix of its own: each entry declares how long its
// calldata is, and the contract uses that to find the next one, so an entry
// encoded a byte short reinterprets everything after it.
func TestSafeMultisendEncoding(t *testing.T) {
	v := loadRelayerVectors(t)
	got, err := aggregate(safeCalls(v.Safe.Many.Transactions), v.Contracts.SafeMultisend)
	if err != nil {
		t.Fatal(err)
	}
	if got.To != v.Safe.Multisend.To {
		t.Errorf("target = %s, want the multisend %s", got.To, v.Safe.Multisend.To)
	}
	if got.Data != v.Safe.Multisend.Data {
		t.Errorf("calldata =\n\t%s\nwant\n\t%s", got.Data, v.Safe.Multisend.Data)
	}
	if got.Operation != OpDelegateCall {
		t.Errorf("operation = %d, want a delegate call: a batch that only calls does nothing as the Safe", got.Operation)
	}
}

// TestProxyTransactionMatchesTheOfficialClient covers the legacy proxy path.
func TestProxyTransactionMatchesTheOfficialClient(t *testing.T) {
	v := loadRelayerVectors(t)
	key, err := polymarket.NewPrivateKey(hardhatKey)
	if err != nil {
		t.Fatal(err)
	}
	tc := v.Proxy.One
	got, err := BuildProxyTransaction(key, ProxyParams{
		Owner:    v.Owner,
		Nonce:    tc.Nonce,
		Calls:    proxyCalls(tc.Calls),
		Relay:    tc.Relay,
		GasPrice: tc.GasPrice,
		GasLimit: tc.GasLimit,
		ChainID:  v.ChainID,
	}, v.contracts())
	if err != nil {
		t.Fatal(err)
	}
	assertRequest(t, got, tc.Request)

	// Unlike the Safe, the relay hub takes the signature as it comes.
	if last := got.Signature[len(got.Signature)-2:]; last != "1b" && last != "1c" {
		t.Errorf("v byte is %q, want 1b or 1c: a proxy signature is not re-tagged", last)
	}
}

// TestProxyCalldataEncoding pins the ABI encoding of the factory's call
// array. The tuple holds a bytes member, so the array is an array of offsets
// rather than of values, and every offset is measured from a different place.
func TestProxyCalldataEncoding(t *testing.T) {
	v := loadRelayerVectors(t)
	cases := map[string]vectorProxyCalldata{
		"one call":  {Calls: v.Proxy.One.Calls, Data: v.Proxy.One.Request.Data},
		"two calls": v.Proxy.CalldataTwo,
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := encodeProxyCalls(proxyCalls(tc.Calls))
			if err != nil {
				t.Fatal(err)
			}
			if hexOrEmpty(got) != tc.Data {
				t.Errorf("calldata =\n\t%s\nwant\n\t%s", hexOrEmpty(got), tc.Data)
			}
		})
	}
}

// TestWalletBatchMatchesTheOfficialClient covers the deposit wallet, the
// account form every new Polymarket account uses.
//
// The empty batch is not a curiosity. An EIP-712 array hashes to the hash of
// its elements concatenated, so an empty one hashes the empty string rather
// than contributing nothing, and an implementation that skips it produces a
// different digest for every batch.
func TestWalletBatchMatchesTheOfficialClient(t *testing.T) {
	v := loadRelayerVectors(t)
	key, err := polymarket.NewPrivateKey(hardhatKey)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]vectorDepositCase{
		"several calls": v.DepositWallet.Many,
		"one call":      v.DepositWallet.One,
		"no calls":      v.DepositWallet.Empty,
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			calls := make([]Call, len(tc.Calls))
			for i, c := range tc.Calls {
				calls[i] = Call{To: c.Target, Value: c.Value, Data: c.Data}
			}
			got, err := BuildWalletBatch(key, BatchParams{
				Owner:    v.Owner,
				Wallet:   v.Contracts.Wallet,
				Nonce:    tc.Nonce,
				Deadline: tc.Deadline,
				Calls:    calls,
				ChainID:  v.ChainID,
			}, v.contracts())
			if err != nil {
				t.Fatal(err)
			}
			assertRequest(t, got, tc.Request)

			if got.Deposit == nil {
				t.Fatal("no deposit wallet parameters")
			}
			if got.Deposit.DepositWallet != tc.Request.Deposit.DepositWallet {
				t.Errorf("wallet = %s, want %s", got.Deposit.DepositWallet, tc.Request.Deposit.DepositWallet)
			}
			if len(got.Deposit.Calls) != len(tc.Request.Deposit.Calls) {
				t.Fatalf("%d calls, want %d", len(got.Deposit.Calls), len(tc.Request.Deposit.Calls))
			}
			for i, call := range got.Deposit.Calls {
				if call != tc.Request.Deposit.Calls[i] {
					t.Errorf("call %d = %+v, want %+v", i, call, tc.Request.Deposit.Calls[i])
				}
			}
		})
	}
}

// TestBatchTypedDataIsLegible checks what an external signer is shown for a
// batch. This is the one relayer transaction that is ordinary typed data, so
// a hardware wallet or a policy engine can read the calls it is authorising
// rather than a hash.
func TestBatchTypedDataIsLegible(t *testing.T) {
	v := loadRelayerVectors(t)
	tc := v.DepositWallet.Many
	calls := make([]Call, len(tc.Calls))
	for i, c := range tc.Calls {
		calls[i] = Call{To: c.Target, Value: c.Value, Data: c.Data}
	}
	td, err := BatchTypedData(BatchParams{
		Owner: v.Owner, Wallet: v.Contracts.Wallet,
		Nonce: tc.Nonce, Deadline: tc.Deadline, Calls: calls, ChainID: v.ChainID,
	}, v.contracts())
	if err != nil {
		t.Fatal(err)
	}

	typeString, err := td.TypeString()
	if err != nil {
		t.Fatal(err)
	}
	const want = "Batch(address wallet,uint256 nonce,uint256 deadline,Call[] calls)" +
		"Call(address target,uint256 value,bytes data)"
	if typeString != want {
		t.Errorf("type string =\n\t%s\nwant\n\t%s", typeString, want)
	}
	if td.Domain.VerifyingContract != v.Contracts.Wallet {
		t.Errorf("verifying contract = %s, want the wallet %s",
			td.Domain.VerifyingContract, v.Contracts.Wallet)
	}
	if td.Domain.Name != DepositWalletDomainName {
		t.Errorf("domain name = %q, want %q", td.Domain.Name, DepositWalletDomainName)
	}
}

// TestSafeDomainOmitsNameAndVersion pins the shape that is easiest to get
// wrong by being helpful. A Safe's domain is two fields; adding a name or a
// version changes the separator and the Safe recovers a different address
// from the same signature.
func TestSafeDomainOmitsNameAndVersion(t *testing.T) {
	v := loadRelayerVectors(t)
	td, err := SafeTypedData(SafeParams{
		Owner:   v.Owner,
		Nonce:   v.Safe.One.Nonce,
		Calls:   safeCalls(v.Safe.One.Transactions),
		ChainID: v.ChainID,
	}, v.contracts())
	if err != nil {
		t.Fatal(err)
	}
	if td.Domain.Name != "" || td.Domain.Version != "" {
		t.Errorf("domain carries name %q and version %q, want neither",
			td.Domain.Name, td.Domain.Version)
	}
	if got := len(td.Domain.DomainType()); got != 2 {
		t.Errorf("domain type has %d fields, want 2", got)
	}
	typeString, err := td.TypeString()
	if err != nil {
		t.Fatal(err)
	}
	const want = "SafeTx(address to,uint256 value,bytes data,uint8 operation," +
		"uint256 safeTxGas,uint256 baseGas,uint256 gasPrice,address gasToken," +
		"address refundReceiver,uint256 nonce)"
	if typeString != want {
		t.Errorf("type string =\n\t%s\nwant\n\t%s", typeString, want)
	}
}

// A badCallCase is one malformed call and what makes it malformed.
type badCallCase struct {
	name string
	call Call
}

// TestBuildersRejectMalformedCalls checks that a call that cannot be encoded
// is refused rather than encoded as something else. A signature is made over
// whatever these produce, so a value silently read as zero is a signed
// instruction the caller did not give.
func TestBuildersRejectMalformedCalls(t *testing.T) {
	v := loadRelayerVectors(t)
	key, err := polymarket.NewPrivateKey(hardhatKey)
	if err != nil {
		t.Fatal(err)
	}
	contracts := v.contracts()
	cases := []badCallCase{
		{"no target", Call{Value: "0", Data: "0x"}},
		{"a target that is not an address", Call{To: "0x1234", Value: "0", Data: "0x"}},
		{"a value that is not a number", Call{To: v.Contracts.Wallet, Value: "lots", Data: "0x"}},
		{"a negative value", Call{To: v.Contracts.Wallet, Value: "-1", Data: "0x"}},
		{"calldata that is not hex", Call{To: v.Contracts.Wallet, Value: "0", Data: "0xzz"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := BuildSafeTransaction(key, SafeParams{
				Owner: v.Owner, Nonce: "1", Calls: []Call{tc.call}, ChainID: v.ChainID,
			}, contracts); err == nil {
				t.Error("the Safe builder signed it")
			}
			if _, err := BuildProxyTransaction(key, ProxyParams{
				Owner: v.Owner, Nonce: "1", Calls: []Call{tc.call},
				Relay: v.Contracts.Relay, ChainID: v.ChainID,
			}, contracts); err == nil {
				t.Error("the proxy builder signed it")
			}
			if _, err := BuildWalletBatch(key, BatchParams{
				Owner: v.Owner, Wallet: v.Contracts.Wallet, Nonce: "1",
				Deadline: "1800000000", Calls: []Call{tc.call}, ChainID: v.ChainID,
			}, contracts); err == nil {
				t.Error("the batch builder signed it")
			}
		})
	}

	// A transaction with nothing in it is a nonce spent for no reason, except
	// for a deposit wallet batch where the relayer client allows it.
	if _, err := BuildSafeTransaction(key, SafeParams{
		Owner: v.Owner, Nonce: "1", ChainID: v.ChainID,
	}, contracts); err == nil {
		t.Error("the Safe builder signed an empty transaction")
	}
	if _, err := BuildProxyTransaction(key, ProxyParams{
		Owner: v.Owner, Nonce: "1", Relay: v.Contracts.Relay, ChainID: v.ChainID,
	}, contracts); err == nil {
		t.Error("the proxy builder signed an empty transaction")
	}
}

// TestBuildersNeedASigner checks the nil case, which would otherwise panic
// somewhere less obvious.
func TestBuildersNeedASigner(t *testing.T) {
	v := loadRelayerVectors(t)
	contracts := v.contracts()
	calls := []Call{{To: v.Contracts.Wallet, Value: "0", Data: "0x"}}
	if _, err := BuildSafeTransaction(nil, SafeParams{Owner: v.Owner, Nonce: "1", Calls: calls}, contracts); err == nil {
		t.Error("built a Safe transaction with no signer")
	}
	if _, err := BuildProxyTransaction(nil, ProxyParams{Owner: v.Owner, Nonce: "1", Calls: calls, Relay: v.Contracts.Relay}, contracts); err == nil {
		t.Error("built a proxy transaction with no signer")
	}
	if _, err := BuildWalletBatch(nil, BatchParams{Owner: v.Owner, Wallet: v.Contracts.Wallet, Nonce: "1", Deadline: "1", Calls: calls}, contracts); err == nil {
		t.Error("built a batch with no signer")
	}
}

// assertRequest compares the parts of a built request that the wallet checks.
func assertRequest(t *testing.T, got SubmitRequest, want vectorRequest) {
	t.Helper()
	if string(got.Type) != want.Type {
		t.Errorf("type = %s, want %s", got.Type, want.Type)
	}
	if got.From != want.From {
		t.Errorf("from = %s, want %s", got.From, want.From)
	}
	if got.To != want.To {
		t.Errorf("to = %s, want %s", got.To, want.To)
	}
	if got.ProxyWallet != want.ProxyWallet {
		t.Errorf("wallet = %s, want %s", got.ProxyWallet, want.ProxyWallet)
	}
	if got.Data != want.Data {
		t.Errorf("data =\n\t%s\nwant\n\t%s", got.Data, want.Data)
	}
	if got.Nonce != want.Nonce {
		t.Errorf("nonce = %s, want %s", got.Nonce, want.Nonce)
	}
	if got.Signature != want.Signature {
		t.Errorf("signature =\n\t%s\nwant\n\t%s", got.Signature, want.Signature)
	}
}
