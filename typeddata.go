// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strings"

	"github.com/ChloePike/go-polymarket/internal/eip712"
)

// This file exposes what an order actually says, not just the hash of what it
// says.
//
// A Signer that only ever sees a 32-byte digest cannot show anyone what is
// being authorised: a hardware wallet has nothing to render, an audit log
// records an opaque hash, and a policy engine cannot refuse an order for being
// too large because it cannot see the size. EIP-712 exists precisely so that a
// signer can display structured data, and TypedData is that structure.
//
// The digest has exactly one implementation, TypedData.Digest. OrderDigest and
// ClobAuthDigest are built on it rather than beside it, so the bytes an
// auditor reads and the bytes a wallet signs cannot drift apart.

// A TypedDataField is one field of an EIP-712 struct type: its name and its
// Solidity type.
type TypedDataField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// A TypedDataDomain is the EIP-712 domain a signature is bound to. An empty
// field means the domain omits it entirely — it is absent from the type
// string, not zeroed.
//
// Polymarket relies on this twice. The level-1 authentication domain carries
// no verifying contract, and a Gnosis Safe transaction carries neither a name
// nor a version. Hashing an absent field as an empty string gives a different
// separator and a signature nothing accepts.
type TypedDataDomain struct {
	Name              string `json:"name,omitempty"`
	Version           string `json:"version,omitempty"`
	ChainID           int64  `json:"chainId"`
	VerifyingContract string `json:"verifyingContract,omitempty"`
}

// TypedData is an EIP-712 payload in the shape `eth_signTypedData_v4` takes,
// so it marshals straight into what a wallet, an HSM or an audit log expects:
//
//	td, err := polymarket.OrderTypedData(order, polymarket.ChainPolygon, opts)
//	json.NewEncoder(auditLog).Encode(td)
//	digest, err := td.Digest()
//
// Message holds the field values keyed by name. It is a map on purpose: the
// order fields are hashed in is taken from Types[PrimaryType], never from the
// map, so map iteration order is irrelevant and must not be "fixed".
type TypedData struct {
	Types       map[string][]TypedDataField `json:"types"`
	PrimaryType string                      `json:"primaryType"`
	Domain      TypedDataDomain             `json:"domain"`
	Message     map[string]any              `json:"message"`
}

// DomainType returns the EIP712Domain field list this domain actually has.
//
// A domain with no verifying contract has a three-field type string, and
// hashing it as four fields with a zero address gives a different, wrong
// separator. Use it when building a TypedData by hand, so that the declared
// domain type and the domain agree.
func (d TypedDataDomain) DomainType() []TypedDataField {
	var fields []TypedDataField
	if d.Name != "" {
		fields = append(fields, TypedDataField{Name: "name", Type: "string"})
	}
	if d.Version != "" {
		fields = append(fields, TypedDataField{Name: "version", Type: "string"})
	}
	fields = append(fields, TypedDataField{Name: "chainId", Type: "uint256"})
	if d.VerifyingContract != "" {
		fields = append(fields, TypedDataField{Name: "verifyingContract", Type: "address"})
	}
	return fields
}

func (d TypedDataDomain) eip712Domain() eip712.Domain {
	return eip712.Domain{
		Name:              d.Name,
		Version:           d.Version,
		ChainID:           big.NewInt(d.ChainID),
		VerifyingContract: d.VerifyingContract,
	}
}

// DomainSeparator returns hashStruct(EIP712Domain) for this payload.
func (td TypedData) DomainSeparator() ([32]byte, error) {
	sep, err := td.Domain.eip712Domain().Separator()
	if err != nil {
		return [32]byte{}, err
	}
	return sep, nil
}

// TypeString renders the EIP-712 encodeType string for the primary type, the
// exact text whose hash goes into hashStruct. It is worth logging next to a
// signature: two payloads that differ only in their type string produce
// different digests for identical-looking messages.
//
// A struct that refers to other structs carries their definitions too, sorted
// by name after the primary one — the order EIP-712 fixes, not the order the
// Types map happens to iterate in.
func (td TypedData) TypeString() (string, error) {
	return td.encodeType(td.PrimaryType)
}

// encodeType is EIP-712 encodeType: the named struct, then every struct it
// reaches, sorted by name.
func (td TypedData) encodeType(name string) (string, error) {
	referenced := make(map[string]bool)
	if err := td.collectTypes(name, referenced); err != nil {
		return "", err
	}
	delete(referenced, name)

	names := make([]string, 0, len(referenced))
	for n := range referenced {
		names = append(names, n)
	}
	sort.Strings(names)

	s, err := td.typeSegment(name)
	if err != nil {
		return "", err
	}
	for _, n := range names {
		segment, err := td.typeSegment(n)
		if err != nil {
			return "", err
		}
		s += segment
	}
	return s, nil
}

// typeSegment renders one struct as "Name(type field,type field)".
func (td TypedData) typeSegment(name string) (string, error) {
	fields, ok := td.Types[name]
	if !ok {
		return "", fmt.Errorf("polymarket: typed data has no type %q", name)
	}
	s := name + "("
	for i, f := range fields {
		if i > 0 {
			s += ","
		}
		s += f.Type + " " + f.Name
	}
	return s + ")", nil
}

// collectTypes walks the struct types name reaches, itself included.
func (td TypedData) collectTypes(name string, found map[string]bool) error {
	if found[name] {
		return nil
	}
	fields, ok := td.Types[name]
	if !ok {
		return fmt.Errorf("polymarket: typed data has no type %q", name)
	}
	found[name] = true
	for _, f := range fields {
		base := elementType(f.Type)
		if _, isStruct := td.Types[base]; isStruct {
			if err := td.collectTypes(base, found); err != nil {
				return err
			}
		}
	}
	return nil
}

// StructHash returns hashStruct(primaryType) over the message.
func (td TypedData) StructHash() ([32]byte, error) {
	return td.hashStruct(td.PrimaryType, td.Message)
}

// hashStruct hashes one struct value against its declared type.
func (td TypedData) hashStruct(name string, message map[string]any) ([32]byte, error) {
	fields, ok := td.Types[name]
	if !ok {
		return [32]byte{}, fmt.Errorf("polymarket: typed data has no type %q", name)
	}
	typeString, err := td.encodeType(name)
	if err != nil {
		return [32]byte{}, err
	}

	var enc eip712.Encoder
	for _, f := range fields {
		value, ok := message[f.Name]
		if !ok {
			return [32]byte{}, fmt.Errorf("polymarket: typed data message has no field %q", f.Name)
		}
		word, err := td.encodeValue(f.Type, value)
		if err != nil {
			return [32]byte{}, fmt.Errorf("polymarket: field %s: %w", f.Name, err)
		}
		enc.Word(f.Name, word)
	}
	return enc.StructHash(eip712.TypeHash(typeString))
}

// encodeValue encodes one message value as its declared type: a nested struct
// by its own hashStruct, an array by the hash of its encoded elements, and
// anything else atomically.
func (td TypedData) encodeValue(solidityType string, value any) (eip712.Word, error) {
	if element, isArray := arrayElement(solidityType); isArray {
		items, err := valueSlice(value)
		if err != nil {
			return eip712.Word{}, err
		}
		words := make([]eip712.Word, len(items))
		for i, item := range items {
			if words[i], err = td.encodeValue(element, item); err != nil {
				return eip712.Word{}, fmt.Errorf("element %d: %w", i, err)
			}
		}
		return eip712.Array(words), nil
	}
	if _, isStruct := td.Types[solidityType]; isStruct {
		message, err := valueMessage(value)
		if err != nil {
			return eip712.Word{}, err
		}
		return td.hashStruct(solidityType, message)
	}
	return encodeTypedValue(solidityType, value)
}

// arrayElement splits one array suffix off a type, reporting whether there was
// one. Both "Call[]" and "uint256[3]" name an array; EIP-712 encodes them the
// same way, as the hash of the concatenated elements.
func arrayElement(solidityType string) (string, bool) {
	if !strings.HasSuffix(solidityType, "]") {
		return solidityType, false
	}
	open := strings.LastIndexByte(solidityType, '[')
	if open < 0 {
		return solidityType, false
	}
	return solidityType[:open], true
}

// elementType strips every array suffix, leaving the type an array is of.
func elementType(solidityType string) string {
	for {
		base, isArray := arrayElement(solidityType)
		if !isArray {
			return base
		}
		solidityType = base
	}
}

// valueSlice reads a value that must be a list. Reflection rather than a type
// switch, because the elements may be any struct-shaped map or scalar and the
// caller should not have to build a []any to be understood.
func valueSlice(value any) ([]any, error) {
	v := reflect.ValueOf(value)
	if !v.IsValid() || (v.Kind() != reflect.Slice && v.Kind() != reflect.Array) {
		return nil, fmt.Errorf("got %T, want a list", value)
	}
	items := make([]any, v.Len())
	for i := range items {
		items[i] = v.Index(i).Interface()
	}
	return items, nil
}

// valueMessage reads a value that must be a nested struct.
func valueMessage(value any) (map[string]any, error) {
	m, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("got %T, want a map[string]any", value)
	}
	return m, nil
}

// Digest returns the 32 bytes a signature covers:
//
//	keccak256(0x19 ‖ 0x01 ‖ domainSeparator ‖ hashStruct(message))
//
// This is the only place the digest is computed. Everything else in this
// package that needs one goes through here.
func (td TypedData) Digest() ([32]byte, error) {
	separator, err := td.DomainSeparator()
	if err != nil {
		return [32]byte{}, err
	}
	structHash, err := td.StructHash()
	if err != nil {
		return [32]byte{}, err
	}
	return eip712.Digest(separator, structHash), nil
}

// encodeTypedValue encodes one message value according to its declared
// Solidity type.
//
// It handles exactly the five types Polymarket signs and rejects anything
// else. Silently skipping an unrecognised type would hash a struct with a
// field missing, which is a valid signature over an order nobody wrote.
func encodeTypedValue(solidityType string, value any) (eip712.Word, error) {
	switch solidityType {
	case "address":
		s, err := typedString(value)
		if err != nil {
			return eip712.Word{}, err
		}
		return eip712.Address(s)

	case "bytes32":
		s, err := typedString(value)
		if err != nil {
			return eip712.Word{}, err
		}
		return eip712.Bytes32(s)

	case "string":
		s, err := typedString(value)
		if err != nil {
			return eip712.Word{}, err
		}
		return eip712.String(s), nil

	case "bytes":
		// Dynamic, so hashed rather than padded — the opposite of bytes32
		// above, which is atomic and carried verbatim. Calldata arrives as a
		// hex string of arbitrary length.
		s, err := typedString(value)
		if err != nil {
			return eip712.Word{}, err
		}
		b, err := eip712.HexBytes(s)
		if err != nil {
			return eip712.Word{}, err
		}
		return eip712.Bytes(b), nil

	case "bool":
		b, ok := value.(bool)
		if !ok {
			return eip712.Word{}, fmt.Errorf("got %T, want a bool", value)
		}
		if b {
			return eip712.Uint8(1), nil
		}
		return eip712.Uint8(0), nil

	case "uint8", "uint16", "uint32", "uint64", "uint128", "uint256":
		n, err := typedUint(value)
		if err != nil {
			return eip712.Word{}, err
		}
		return eip712.Uint(n)
	}
	return eip712.Word{}, fmt.Errorf("unsupported type %q", solidityType)
}

// typedString reads a value that must be text.
func typedString(value any) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("got %T, want a string", value)
	}
	return s, nil
}

// typedUint reads an unsigned integer from any of the shapes a message can
// carry it in: a decimal string, a Go integer, a big.Int, or the float64 and
// json.Number a JSON round trip produces.
func typedUint(value any) (*big.Int, error) {
	switch v := value.(type) {
	case string:
		n, ok := new(big.Int).SetString(v, 10)
		if !ok {
			return nil, fmt.Errorf("%q is not a decimal integer", v)
		}
		return n, nil
	case json.Number:
		n, ok := new(big.Int).SetString(v.String(), 10)
		if !ok {
			return nil, fmt.Errorf("%q is not a decimal integer", v)
		}
		return n, nil
	case *big.Int:
		return v, nil
	case int:
		return big.NewInt(int64(v)), nil
	case int64:
		return big.NewInt(v), nil
	case uint64:
		return new(big.Int).SetUint64(v), nil
	case uint8:
		return big.NewInt(int64(v)), nil
	case float64:
		// A JSON round trip turns every number into a float64, which stops
		// being exact above 2^53. Refusing here is the same rule that bounds
		// the order salt: a value that cannot round-trip must not be signed.
		if v != math.Trunc(v) {
			return nil, fmt.Errorf("%v is not an integer", v)
		}
		if math.Abs(v) >= 1<<53 {
			return nil, fmt.Errorf("%v exceeds the range a JSON number represents exactly; pass it as a decimal string", v)
		}
		return big.NewInt(int64(v)), nil
	}
	return nil, fmt.Errorf("got %T, want an unsigned integer", value)
}

// OrderTypedData returns the EIP-712 payload an order's signature covers.
//
// Eleven fields are signed. The order's Taker and Expiration travel on the
// wire but are absent here on purpose, because they are absent from the
// signature; an auditor comparing this against a submitted order should
// expect exactly that difference and no other.
//
// Numeric fields are carried as decimal strings rather than JSON numbers, so
// that marshalling this payload and handing it to an external signer cannot
// lose precision on a uint256.
func OrderTypedData(o Order, chainID int64, opts OrderOptions) (TypedData, error) {
	domain, err := orderDomain(chainID, opts.version(), opts.NegRisk)
	if err != nil {
		return TypedData{}, err
	}
	d := TypedDataDomain{
		Name:              domain.Name,
		Version:           domain.Version,
		ChainID:           chainID,
		VerifyingContract: domain.VerifyingContract,
	}
	return TypedData{
		Types: map[string][]TypedDataField{
			"EIP712Domain": d.DomainType(),
			"Order":        cloneFields(orderStructFields),
		},
		PrimaryType: "Order",
		Domain:      d,
		Message: map[string]any{
			"salt":          o.Salt,
			"maker":         o.Maker,
			"signer":        o.Signer,
			"tokenId":       o.TokenID,
			"makerAmount":   o.MakerAmount,
			"takerAmount":   o.TakerAmount,
			"side":          uint8(o.Side.uint8()),
			"signatureType": uint8(o.SignatureType),
			"timestamp":     o.Timestamp,
			"metadata":      o.Metadata,
			"builder":       o.Builder,
		},
	}, nil
}

// cloneFields returns a private copy of a field list.
//
// The canonical lists below are package state. Handing a caller the slice
// itself would let an edit to one returned payload rewrite the type of every
// payload the process builds afterwards, which is a way to sign the wrong
// struct that no test of a single order would find.
func cloneFields(fields []TypedDataField) []TypedDataField {
	out := make([]TypedDataField, len(fields))
	copy(out, fields)
	return out
}

// orderStructFields is the signed Order struct, in the order it is hashed.
var orderStructFields = []TypedDataField{
	{Name: "salt", Type: "uint256"},
	{Name: "maker", Type: "address"},
	{Name: "signer", Type: "address"},
	{Name: "tokenId", Type: "uint256"},
	{Name: "makerAmount", Type: "uint256"},
	{Name: "takerAmount", Type: "uint256"},
	{Name: "side", Type: "uint8"},
	{Name: "signatureType", Type: "uint8"},
	{Name: "timestamp", Type: "uint256"},
	{Name: "metadata", Type: "bytes32"},
	{Name: "builder", Type: "bytes32"},
}

// clobAuthStructFields is the level-1 authentication struct. Note that
// timestamp is a string even though it holds a number: EIP-712 hashes a
// string field rather than padding it, and encoding it as a number
// authenticates as nobody.
var clobAuthStructFields = []TypedDataField{
	{Name: "address", Type: "address"},
	{Name: "timestamp", Type: "string"},
	{Name: "nonce", Type: "uint256"},
	{Name: "message", Type: "string"},
}

// ClobAuthTypedData returns the EIP-712 payload the level-1 handshake signs.
// Its domain deliberately carries no verifying contract.
func ClobAuthTypedData(address string, chainID int64, timestamp string, nonce int64) TypedData {
	d := TypedDataDomain{
		Name:    clobAuthDomainName,
		Version: clobAuthDomainVersion,
		ChainID: chainID,
	}
	return TypedData{
		Types: map[string][]TypedDataField{
			"EIP712Domain": d.DomainType(),
			"ClobAuth":     cloneFields(clobAuthStructFields),
		},
		PrimaryType: "ClobAuth",
		Domain:      d,
		Message: map[string]any{
			"address":   address,
			"timestamp": timestamp,
			"nonce":     nonce,
			"message":   clobAuthMessage,
		},
	}
}
