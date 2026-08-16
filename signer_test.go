// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package polymarket

import (
	"encoding/hex"
	"testing"
)

func TestPrivateKeyAddress(t *testing.T) {
	g := loadGolden(t)
	if len(g.Accounts) == 0 {
		t.Fatal("golden accounts are empty")
	}
	for _, a := range g.Accounts {
		key, err := NewPrivateKey(a.PrivateKey)
		if err != nil {
			t.Fatalf("%s: %v", a.PrivateKey, err)
		}
		if got := key.Address(); got != a.Address {
			t.Errorf("address(%s) = %s, want %s", a.PrivateKey, got, a.Address)
		}
	}
}

// badKeyCase is one private key NewPrivateKey must reject.
type badKeyCase struct {
	name string
	key  string
}

func TestPrivateKeyErrors(t *testing.T) {
	cases := []badKeyCase{
		{"empty", ""},
		{"short", "0xac09"},
		{"not hex", "0xzzzz974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff8"},
		{"zero", "0x0000000000000000000000000000000000000000000000000000000000000000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewPrivateKey(tc.key); err == nil {
				t.Fatal("got nil error")
			}
		})
	}
}

// TestSignDigest pins the r ‖ s ‖ v byte order against signatures produced by
// the official SDK.
func TestSignDigest(t *testing.T) {
	g := loadGolden(t)
	key, err := NewPrivateKey(g.Accounts[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range g.Orders {
		t.Run(o.Name, func(t *testing.T) {
			raw, err := hex.DecodeString(o.Digest[2:])
			if err != nil {
				t.Fatal(err)
			}
			var digest [32]byte
			copy(digest[:], raw)

			sig, err := key.SignDigest(digest)
			if err != nil {
				t.Fatal(err)
			}
			if got := "0x" + hex.EncodeToString(sig); got != o.Signature {
				t.Fatalf("signature = %s, want %s", got, o.Signature)
			}
			if v := sig[64]; v != 27 && v != 28 {
				t.Fatalf("recovery byte = %d, want 27 or 28", v)
			}
		})
	}
}

// checksumCase is one address and its EIP-55 form.
type checksumCase struct {
	in   string
	want string
}

func TestChecksumAddress(t *testing.T) {
	// Expected values are what viem, ethers and go-ethereum all produce. Note
	// that EIP-55's own text prints the fourth one as "...D1220a0c...", which
	// no implementation agrees with; the checksum is the authority, not the
	// prose.
	cases := []checksumCase{
		{"0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed", "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"},
		{"0xfb6916095ca1df60bb79ce92ce3ea74c37c5d359", "0xfB6916095ca1df60bB79Ce92cE3Ea74c37c5d359"},
		{"0xdbf03b407c01e7cd3cbea99509d93f8dddc8c6fb", "0xdbF03B407c01E7cD3CBea99509d93f8DDDC8C6FB"},
		{"0xd1220a0cf47c7b9be7a2e6ba89f429762e7b9adb", "0xD1220A0cf47c7B9Be7A2E6BA89F429762e7b9aDb"},
		{"0x52908400098527886e0f7030069857d2e4169ee7", "0x52908400098527886E0F7030069857D2E4169EE7"},
		{"0xde709f2102306220921060314715629080e2fb77", "0xde709f2102306220921060314715629080e2fb77"},
		// Input case is ignored: an already-checksummed address is idempotent
		// and an all-caps one is normalised.
		{"0x5AAEB6053F3E94C9B9A09F33669435E7EF1BEAED", "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"},
		{"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed", "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"},
		// Malformed input passes through rather than being mangled.
		{"not an address", "not an address"},
		{"0x", "0x"},
		{"0xzzzzb6053f3e94c9b9a09f33669435e7ef1beaed", "0xzzzzb6053f3e94c9b9a09f33669435e7ef1beaed"},
	}
	for _, tc := range cases {
		if got := ChecksumAddress(tc.in); got != tc.want {
			t.Errorf("ChecksumAddress(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
