// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike
//
// gen-vectors.mjs regenerates testdata/vectors.json: the golden vectors that
// pin this Go client byte-for-byte against the official Polymarket TypeScript
// SDK. It is a development tool and is not part of the Go build.
//
// Usage:
//
//	mkdir -p /tmp/pmsdk && cd /tmp/pmsdk && npm init -y
//	npm install --ignore-scripts @polymarket/clob-client-v2@1.1.0
//	node <repo>/testdata/gen-vectors.mjs /tmp/pmsdk > <repo>/testdata/vectors.json
//
// The private key below is the well-known Hardhat/Anvil development account
// number 0. It is public, holds nothing, and exists only to make signatures
// reproducible.

import { createRequire } from "node:module";

const root = process.argv[2];
if (!root) {
	console.error("usage: node gen-vectors.mjs <dir containing node_modules>");
	process.exit(2);
}
const dist = `${root}/node_modules/@polymarket/clob-client-v2/dist`;

// Every import is by file path: this script lives in the Go repository but its
// dependencies live in the throwaway npm tree passed as argv[2], so bare
// package specifiers would not resolve. The clob-client-v2 "exports" map only
// publishes ".", so its internal builders need a path import regardless.
// ethers v5 ships an ESM build with extensionless imports, which Node refuses
// to load; its CommonJS build is the one that resolves cleanly.
const require = createRequire(`${root}/package.json`);
const { Wallet } = require("@ethersproject/wallet");
const { ExchangeOrderBuilderV2, ExchangeOrderBuilderV3 } = await import(`${dist}/order-utils/exchangeOrderBuilderV2.js`);
const { ExchangeOrderBuilderV1 } = await import(`${dist}/order-utils/exchangeOrderBuilderV1.js`);
const { getOrderRawAmounts } = await import(`${dist}/order-builder/helpers/getOrderRawAmounts.js`);
const { getMarketOrderRawAmounts } = await import(`${dist}/order-builder/helpers/getMarketOrderRawAmounts.js`);
const { ROUNDING_CONFIG } = await import(`${dist}/order-builder/helpers/roundingConfig.js`);
const { buildClobEip712Signature } = await import(`${dist}/signing/eip712.js`);
const { buildPolyHmacSignature } = await import(`${dist}/signing/hmac.js`);
const { orderToJsonV2 } = await import(`${dist}/types/ordersV2.js`);
const { getContractConfig } = await import(`${dist}/config.js`);
const viem = require("viem");
const { hashTypedData, keccak256, toHex, parseUnits } = viem;

const sdkVersion = require(`${root}/node_modules/@polymarket/clob-client-v2/package.json`).version;

const PRIV = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80";
const wallet = new Wallet(PRIV);
const ADDR = await wallet.getAddress();

const CHAIN = 137;
const cfg = getContractConfig(CHAIN);

const ORDER_TYPE_STRING =
	"Order(uint256 salt,address maker,address signer,uint256 tokenId,uint256 makerAmount," +
	"uint256 takerAmount,uint8 side,uint8 signatureType,uint256 timestamp,bytes32 metadata,bytes32 builder)";
const DOMAIN_TYPE_STRING = "EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)";

const ORDER_STRUCT = [
	{ name: "salt", type: "uint256" },
	{ name: "maker", type: "address" },
	{ name: "signer", type: "address" },
	{ name: "tokenId", type: "uint256" },
	{ name: "makerAmount", type: "uint256" },
	{ name: "takerAmount", type: "uint256" },
	{ name: "side", type: "uint8" },
	{ name: "signatureType", type: "uint8" },
	{ name: "timestamp", type: "uint256" },
	{ name: "metadata", type: "bytes32" },
	{ name: "builder", type: "bytes32" },
];

const TOKEN = "71321045679252212594626385532706912750332728571942532289631379312455583992563";
const BUILDER_CODE = "0x11adfa1337e1d4049b93be13548465015ac613efe3f8e7cee2347170f4ae5417";
const ZERO32 = "0x0000000000000000000000000000000000000000000000000000000000000000";

// ---------------------------------------------------------------- orders

// Fixed salt and timestamp make every vector reproducible.
const SALT = "479249096354";
const TS = "1740000000000";

function exchangeFor(version, negRisk) {
	if (version === 1) return negRisk ? cfg.negRiskExchange : cfg.exchange;
	if (version === 2) return negRisk ? cfg.negRiskExchangeV2 : cfg.exchangeV2;
	return cfg.exchangeV3;
}

function builderFor(version, address) {
	const salt = () => SALT;
	if (version === 1) return new ExchangeOrderBuilderV1(address, CHAIN, wallet, salt);
	if (version === 2) return new ExchangeOrderBuilderV2(address, CHAIN, wallet, salt);
	return new ExchangeOrderBuilderV3(address, CHAIN, wallet, salt);
}

const orderCases = [
	{ name: "buy_v2_tick_0.01", version: 2, negRisk: false, side: "BUY", price: 0.52, size: 100, tick: "0.01" },
	{ name: "sell_v2_tick_0.01", version: 2, negRisk: false, side: "SELL", price: 0.52, size: 100, tick: "0.01" },
	{ name: "buy_v2_negrisk", version: 2, negRisk: true, side: "BUY", price: 0.33, size: 17.5, tick: "0.01" },
	{ name: "sell_v2_negrisk", version: 2, negRisk: true, side: "SELL", price: 0.67, size: 3.33, tick: "0.001" },
	{ name: "buy_v2_builder_code", version: 2, negRisk: false, side: "BUY", price: 0.5, size: 5, tick: "0.01", builder: BUILDER_CODE },
	{ name: "buy_v2_expiration", version: 2, negRisk: false, side: "BUY", price: 0.25, size: 40, tick: "0.01", expiration: "1800000000" },
	{ name: "buy_v2_tick_0.0001", version: 2, negRisk: false, side: "BUY", price: 0.9999, size: 1.23, tick: "0.0001" },
	{ name: "sell_v2_tick_0.1", version: 2, negRisk: false, side: "SELL", price: 0.7, size: 9.99, tick: "0.1" },
	{ name: "buy_v3", version: 3, negRisk: false, side: "BUY", price: 0.52, size: 100, tick: "0.01" },
	{ name: "sell_v3", version: 3, negRisk: false, side: "SELL", price: 0.41, size: 12.34, tick: "0.001" },
];

const orders = [];
for (const c of orderCases) {
	const rc = ROUNDING_CONFIG[c.tick];
	const { side, rawMakerAmt, rawTakerAmt } = getOrderRawAmounts(c.side, c.size, c.price, rc);
	const makerAmount = parseUnits(rawMakerAmt.toString(), 6).toString();
	const takerAmount = parseUnits(rawTakerAmt.toString(), 6).toString();

	const verifying = exchangeFor(c.version, c.negRisk);
	const b = builderFor(c.version, verifying);
	const signed = await b.buildSignedOrder({
		maker: ADDR,
		signer: ADDR,
		tokenId: TOKEN,
		makerAmount,
		takerAmount,
		side,
		signatureType: 0,
		timestamp: TS,
		metadata: ZERO32,
		builder: c.builder ?? ZERO32,
		expiration: c.expiration ?? "0",
	});

	const domain = {
		name: "Polymarket CTF Exchange",
		version: c.version === 3 ? "3" : "2",
		chainId: CHAIN,
		verifyingContract: verifying,
	};
	const message = {
		salt: signed.salt,
		maker: signed.maker,
		signer: signed.signer,
		tokenId: signed.tokenId,
		makerAmount: signed.makerAmount,
		takerAmount: signed.takerAmount,
		side: signed.side === "BUY" ? 0 : 1,
		signatureType: signed.signatureType,
		timestamp: signed.timestamp,
		metadata: signed.metadata,
		builder: signed.builder,
	};
	const digest = hashTypedData({ domain, types: { Order: ORDER_STRUCT }, primaryType: "Order", message });

	orders.push({
		name: c.name,
		input: {
			version: c.version, negRisk: c.negRisk, side: c.side,
			price: String(c.price), size: String(c.size), tickSize: c.tick,
			tokenId: TOKEN, builderCode: c.builder ?? ZERO32, expiration: c.expiration ?? "0",
		},
		domain,
		rawAmounts: { maker: rawMakerAmt.toString(), taker: rawTakerAmt.toString() },
		order: signed,
		wireJSON: orderToJsonV2(signed, "", c.orderType ?? "GTC"),
		digest,
		signature: signed.signature,
	});
}

// ---------------------------------------------------------------- amounts

// A grid wide enough to expose every rounding artefact, including the ones the
// SDK's float64 arithmetic produces. The Go implementation uses exact rational
// arithmetic, so any divergence here is a deliberate, documented decision.
const amountGrid = [];
const prices = ["0.001", "0.01", "0.019", "0.1", "0.25", "0.333", "0.3333", "0.5", "0.52", "0.6667", "0.75", "0.9", "0.99", "0.999", "0.9999"];
const sizes = ["1", "1.5", "3", "3.33", "5", "7.77", "10", "12.34", "33.33", "100", "123.45", "1000", "0.01", "0.99"];
const ticks = ["0.1", "0.01", "0.005", "0.0025", "0.001", "0.0001"];
for (const tick of ticks) {
	const rc = ROUNDING_CONFIG[tick];
	for (const p of prices) {
		for (const s of sizes) {
			for (const side of ["BUY", "SELL"]) {
				const price = Number(p), size = Number(s);
				const r = getOrderRawAmounts(side, size, price, rc);
				let makerAmount, takerAmount, err = null;
				try {
					makerAmount = parseUnits(r.rawMakerAmt.toString(), 6).toString();
					takerAmount = parseUnits(r.rawTakerAmt.toString(), 6).toString();
				} catch (e) {
					err = String(e && e.message).slice(0, 120);
				}
				amountGrid.push({
					side, price: p, size: s, tickSize: tick,
					rawMaker: r.rawMakerAmt.toString(), rawTaker: r.rawTakerAmt.toString(),
					makerAmount: makerAmount ?? null, takerAmount: takerAmount ?? null,
					error: err,
				});
			}
		}
	}
}

// ---------------------------------------------------------- market orders

const marketCases = [];
for (const tick of ticks) {
	const rc = ROUNDING_CONFIG[tick];
	for (const [side, amount, price] of [
		["BUY", 100, 0.52], ["BUY", 33.33, 0.1], ["BUY", 1, 0.9999],
		["SELL", 100, 0.52], ["SELL", 12.5, 0.33], ["SELL", 7.77, 0.6667],
	]) {
		const r = getMarketOrderRawAmounts(side, amount, price, rc);
		marketCases.push({
			side, amount: String(amount), price: String(price), tickSize: tick,
			rawMaker: r.rawMakerAmt.toString(), rawTaker: r.rawTakerAmt.toString(),
			makerAmount: parseUnits(r.rawMakerAmt.toString(), 6).toString(),
			takerAmount: parseUnits(r.rawTakerAmt.toString(), 6).toString(),
		});
	}
}

// -------------------------------------------------------------- clob auth

const clobAuth = [];
for (const [ts, nonce] of [["1740000000", 0], ["1700000123", 7], ["1", 0]]) {
	const sig = await buildClobEip712Signature(wallet, CHAIN, ts, nonce, ADDR);
	const domain = { name: "ClobAuthDomain", version: "1", chainId: CHAIN };
	const types = {
		ClobAuth: [
			{ name: "address", type: "address" },
			{ name: "timestamp", type: "string" },
			{ name: "nonce", type: "uint256" },
			{ name: "message", type: "string" },
		],
	};
	const message = {
		address: ADDR, timestamp: String(ts), nonce,
		message: "This message attests that I control the given wallet",
	};
	clobAuth.push({
		chainId: CHAIN, address: ADDR, timestamp: String(ts), nonce,
		message: message.message,
		digest: hashTypedData({ domain, types, primaryType: "ClobAuth", message }),
		signature: sig,
	});
}

// ------------------------------------------------------------------ hmac

const SECRET = "PLoJhxT8V3PMEHtGZFLD9YfKKW3Kx0QfC5wY1qkq_iM=";
const hmac = [];
for (const [ts, method, path, body] of [
	["1740000000", "GET", "/order", ""],
	["1740000000", "POST", "/order", '{"a":1}'],
	["1700000123", "GET", "/data/orders?market=0xabc", ""],
	["1", "DELETE", "/order", '{"orderID":"0xdead"}'],
]) {
	hmac.push({
		secret: SECRET, timestamp: String(ts), method, requestPath: path, body,
		signature: await buildPolyHmacSignature(SECRET, Number(ts), method, path, body || undefined),
	});
}


// ------------------------------------------------------- order book hashes

// The websocket market channel identifies a book snapshot by a SHA-1 over the
// summary with its own hash field blanked, so a client that keeps a local book
// can tell whether it has drifted. The digest is taken over JSON.stringify of
// the parsed object, which means the FIELD ORDER of the server's response is
// part of the input. These vectors carry the raw bytes as served alongside the
// hash, so the Go side has something exact to reproduce.
const bookTokens = [
	"32338220190071351435772801779725302244575775216413325951443816017994629993401",
	"25659310674993675562345759665114759892400026242514633218387667107987341231962",
	"54533043819946592547517511176940999955633860128497669742211153063842200957669",
];
const orderBookHashes = [];
for (const tokenId of bookTokens) {
	const res = await fetch(`https://clob.polymarket.com/book?token_id=${tokenId}`);
	if (!res.ok) continue;
	const raw = await res.text();
	const book = JSON.parse(raw);
	const served = book.hash;
	book.hash = "";
	const bytes = new TextEncoder().encode(JSON.stringify(book));
	const digest = await globalThis.crypto.subtle.digest("SHA-1", bytes);
	const hash = Array.from(new Uint8Array(digest)).map((b) => b.toString(16).padStart(2, "0")).join("");
	orderBookHashes.push({
		tokenId,
		fieldOrder: Object.keys(book),
		raw,
		servedHash: served ?? null,
		hash,
		canonical: JSON.stringify(book),
	});
}

// --------------------------------------------------------------- accounts

const accounts = [];
for (const k of [
	PRIV,
	"0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d",
	"0x0000000000000000000000000000000000000000000000000000000000000001",
	"0x4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318",
]) {
	accounts.push({ privateKey: k, address: await new Wallet(k).getAddress() });
}

// ----------------------------------------------------------------- output

console.log(JSON.stringify({
	source: `@polymarket/clob-client-v2@${sdkVersion}`,
	note: "Generated by testdata/gen-vectors.mjs. Protocol facts captured from the official SDK; no third-party source is reproduced here.",
	chainId: CHAIN,
	contracts: cfg,
	typeStrings: { order: ORDER_TYPE_STRING, domain: DOMAIN_TYPE_STRING },
	typeHashes: {
		order: keccak256(toHex(ORDER_TYPE_STRING)),
		domain: keccak256(toHex(DOMAIN_TYPE_STRING)),
	},
	roundingConfig: ROUNDING_CONFIG,
	accounts,
	orders,
	amounts: amountGrid,
	marketOrders: marketCases,
	clobAuth,
	hmac,
	orderBookHashes,
}, null, 1));
