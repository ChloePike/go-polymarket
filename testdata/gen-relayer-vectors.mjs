// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike
//
// gen-relayer-vectors.mjs regenerates testdata/relayer-vectors.json: the
// golden vectors that pin the gasless meta-transactions this client signs
// against Polymarket's own relayer client. It is a development tool and is not
// part of the Go build.
//
// Usage:
//
//	mkdir -p /tmp/pmrelayer && cd /tmp/pmrelayer && npm init -y
//	npm install --ignore-scripts @polymarket/builder-relayer-client@0.0.10 viem
//	node <repo>/testdata/gen-relayer-vectors.mjs /tmp/pmrelayer > <repo>/testdata/relayer-vectors.json
//
// It is a separate file from gen-vectors.mjs because it drives a separate SDK.
//
// The private key below is the well-known Hardhat/Anvil development account
// number 0. It is public, holds nothing, and exists only to make signatures
// reproducible.

import { createRequire } from "node:module";

const root = process.argv[2];
if (!root) {
	console.error("usage: node gen-relayer-vectors.mjs <dir containing node_modules>");
	process.exit(2);
}
const require = createRequire(`${root}/package.json`);
const relayer = require("@polymarket/builder-relayer-client");
const viem = require("viem");
const { privateKeyToAccount } = require("viem/accounts");
const { toBytes, encodeFunctionData } = viem;

const {
	buildSafeTransactionRequest,
	buildProxyTransactionRequest,
	buildDepositWalletBatchRequest,
	createSafeMultisendTransaction,
	encodeProxyTransactionData,
} = relayer;

const sdkVersion = require(`${root}/node_modules/@polymarket/builder-relayer-client/package.json`).version;

// The Safe and proxy builders log their finished request. Take stdout back so
// the output of this script is JSON and nothing else.
const emit = console.log;
console.log = () => {};

const PRIV = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80";
const account = privateKeyToAccount(PRIV);
const ADDR = account.address;
const CHAIN = 137;

// The minimum of the relayer client's signer interface that these builders
// reach. estimateGas would need a node, so the gas limit is always supplied.
const signer = {
	async signMessage(message) {
		return account.signMessage({ message: { raw: toBytes(message) } });
	},
	async signRawMessage(message) {
		return account.signMessage({ message: { raw: message } });
	},
	async signTypedData(domain, types, value, primaryType) {
		return account.signTypedData({ domain, types, primaryType, message: value });
	},
	async getAddress() {
		return ADDR;
	},
	async estimateGas() {
		return 10000000n;
	},
};

const SAFE = {
	SafeFactory: "0xaacFeEa03eb1561C4e67d661e40682Bd20E3541b",
	SafeMultisend: "0xA238CBeb142c10Ef7Ad8442C6D1f9E89e07e7761",
};
const PROXY = {
	ProxyFactory: "0xaB45c5A4B0c941a2F231C04C3f49182e1A254052",
	RelayHub: "0xD216153c06E857cD7f72665E0aF1d7D82172F494",
};
const DEPOSIT = {
	DepositWalletFactory: "0x00000000000Fb5C9ADea0298D729A0CB3823Cc07",
	DepositWalletImplementation: "0x58CA52ebe0DadfdF531Cde7062e76746de4Db1eB",
};

const USDC = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174";
const EXCHANGE = "0xE111180000d2663C0091e4f400237545B87B996B";
const RELAY = "0x1234567890AbcdEF1234567890aBcdef12345678";
const WALLET = "0x94bF330955A0b957662fEaF878dE77bf25f76cD9";

const approveAbi = [{
	name: "approve",
	type: "function",
	stateMutability: "nonpayable",
	inputs: [{ name: "spender", type: "address" }, { name: "value", type: "uint256" }],
	outputs: [{ type: "bool" }],
}];
const APPROVE = encodeFunctionData({ abi: approveAbi, functionName: "approve", args: [EXCHANGE, 1000000n] });

// A Safe with one call sends it straight to its target; with several it
// delegate-calls the multisend contract, which changes the operation code and
// so the struct that is signed.
const safeOne = await buildSafeTransactionRequest(signer, {
	from: ADDR, nonce: "7", chainId: CHAIN,
	transactions: [{ to: USDC, operation: 0, data: APPROVE, value: "0" }],
}, SAFE);

const safeMany = await buildSafeTransactionRequest(signer, {
	from: ADDR, nonce: "8", chainId: CHAIN,
	transactions: [
		{ to: USDC, operation: 0, data: APPROVE, value: "0" },
		{ to: EXCHANGE, operation: 0, data: "0x1234", value: "1" },
	],
}, SAFE);

const proxyCalls = [{ to: USDC, typeCode: "1", data: APPROVE, value: "0" }];
const proxyCallsTwo = [
	{ to: USDC, typeCode: "1", data: APPROVE, value: "0" },
	{ to: EXCHANGE, typeCode: "2", data: "0x1234", value: "5" },
];
const proxyRequest = await buildProxyTransactionRequest(signer, {
	from: ADDR, nonce: "3", gasPrice: "100000000000", gasLimit: "10000000",
	data: encodeProxyTransactionData(proxyCalls), relay: RELAY,
}, PROXY);

const walletCalls = [
	{ target: USDC, value: "0", data: APPROVE },
	{ target: EXCHANGE, value: "2", data: "0x" },
];
const depositMany = await buildDepositWalletBatchRequest(signer, {
	from: ADDR, chainId: CHAIN, walletAddress: WALLET, nonce: "11", deadline: "1800000000",
	calls: walletCalls,
}, DEPOSIT);
const depositOne = await buildDepositWalletBatchRequest(signer, {
	from: ADDR, chainId: CHAIN, walletAddress: WALLET, nonce: "1", deadline: "1800000000",
	calls: [walletCalls[0]],
}, DEPOSIT);
// An empty batch is here because an array of structs hashes to the hash of
// nothing, not to nothing, and the two are easy to confuse.
const depositEmpty = await buildDepositWalletBatchRequest(signer, {
	from: ADDR, chainId: CHAIN, walletAddress: WALLET, nonce: "2", deadline: "1800000000",
	calls: [],
}, DEPOSIT);

emit(JSON.stringify({
	source: `@polymarket/builder-relayer-client@${sdkVersion}`,
	note: "Generated by testdata/gen-relayer-vectors.mjs. Protocol facts captured from the official SDK; no third-party source is reproduced here.",
	chainId: CHAIN,
	owner: ADDR,
	contracts: { ...SAFE, ...PROXY, ...DEPOSIT, relay: RELAY, wallet: WALLET },
	calldata: { approve: APPROVE },
	safe: {
		one: { nonce: "7", transactions: [{ to: USDC, operation: 0, data: APPROVE, value: "0" }], request: safeOne },
		many: {
			nonce: "8",
			transactions: [
				{ to: USDC, operation: 0, data: APPROVE, value: "0" },
				{ to: EXCHANGE, operation: 0, data: "0x1234", value: "1" },
			],
			request: safeMany,
		},
		multisend: createSafeMultisendTransaction([
			{ to: USDC, operation: 0, data: APPROVE, value: "0" },
			{ to: EXCHANGE, operation: 0, data: "0x1234", value: "1" },
		], SAFE.SafeMultisend),
	},
	proxy: {
		one: { nonce: "3", gasPrice: "100000000000", gasLimit: "10000000", relay: RELAY, calls: proxyCalls, request: proxyRequest },
		calldataTwo: { calls: proxyCallsTwo, data: encodeProxyTransactionData(proxyCallsTwo) },
	},
	depositWallet: {
		many: { nonce: "11", deadline: "1800000000", calls: walletCalls, request: depositMany },
		one: { nonce: "1", deadline: "1800000000", calls: [walletCalls[0]], request: depositOne },
		empty: { nonce: "2", deadline: "1800000000", calls: [], request: depositEmpty },
	},
}, null, 1));
