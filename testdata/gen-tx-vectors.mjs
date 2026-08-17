// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike
//
// gen-tx-vectors.mjs regenerates testdata/tx-vectors.json: the golden vectors
// that pin the EIP-1559 transactions this client signs and the calldata it
// builds for the token approvals a Polymarket account needs. It is a
// development tool and is not part of the Go build.
//
// Usage:
//
//	mkdir -p /tmp/pmtx && cd /tmp/pmtx && npm init -y
//	npm install --ignore-scripts viem
//	node <repo>/testdata/gen-tx-vectors.mjs /tmp/pmtx > <repo>/testdata/tx-vectors.json
//
// It is a third generator because it drives a third source: transaction
// encoding is Ethereum's, not Polymarket's, so the reference is a general
// client rather than one of Polymarket's SDKs.
//
// The private key below is the well-known Hardhat/Anvil development account
// number 0. It is public, holds nothing, and exists only to make signatures
// reproducible.

import { createRequire } from "node:module";

const root = process.argv[2];
if (!root) {
	console.error("usage: node gen-tx-vectors.mjs <dir containing node_modules>");
	process.exit(2);
}
const require = createRequire(`${root}/package.json`);
const viem = require("viem");
const accounts = require("viem/accounts");

const privateKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80";
const account = accounts.privateKeyToAccount(privateKey);

// The transactions to sign. They cover both chains, calldata and none, zero
// value and not, and a nonce spread wide enough that both signature parities
// appear.
const transactions = [
	{
		name: "polygon transfer",
		chainId: 137,
		nonce: 0,
		to: "0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
		value: "1000000000000000000",
		data: "0x",
		gas: 21000,
		maxFeePerGas: "100000000000",
		maxPriorityFeePerGas: "30000000000",
	},
	{
		name: "polygon usdc approve",
		chainId: 137,
		nonce: 7,
		to: "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174",
		value: "0",
		data: viem.encodeFunctionData({
			abi: viem.parseAbi(["function approve(address spender, uint256 value)"]),
			args: [
				"0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E",
				viem.maxUint256,
			],
		}),
		gas: 60000,
		maxFeePerGas: "250000000000",
		maxPriorityFeePerGas: "40000000000",
	},
	{
		name: "polygon ctf approval",
		chainId: 137,
		nonce: 12345,
		to: "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045",
		value: "0",
		data: viem.encodeFunctionData({
			abi: viem.parseAbi(["function setApprovalForAll(address operator, bool approved)"]),
			args: ["0xC5d563A36AE78145C45a50134d48A1215220f80a", true],
		}),
		gas: 100000,
		maxFeePerGas: "80000000000",
		maxPriorityFeePerGas: "25000000000",
	},
	{
		name: "amoy zero value no data",
		chainId: 80002,
		nonce: 1,
		to: "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC",
		value: "0",
		data: "0x",
		gas: 21000,
		maxFeePerGas: "1500000000",
		maxPriorityFeePerGas: "1500000000",
	},
	{
		name: "amoy long calldata",
		chainId: 80002,
		nonce: 255,
		to: "0x90F79bf6EB2c4f870365E785982E1f101E93b906",
		value: "12345678901234567890",
		data: "0x" + "ab".repeat(200),
		gas: 500000,
		maxFeePerGas: "3000000000",
		maxPriorityFeePerGas: "2000000000",
	},
	{
		name: "contract creation",
		chainId: 137,
		nonce: 3,
		to: null,
		value: "0",
		data: "0x6080604052",
		gas: 200000,
		maxFeePerGas: "60000000000",
		maxPriorityFeePerGas: "30000000000",
	},
];

const signed = [];
for (const t of transactions) {
	const tx = {
		type: "eip1559",
		chainId: t.chainId,
		nonce: t.nonce,
		to: t.to,
		value: BigInt(t.value),
		data: t.data,
		gas: BigInt(t.gas),
		maxFeePerGas: BigInt(t.maxFeePerGas),
		maxPriorityFeePerGas: BigInt(t.maxPriorityFeePerGas),
	};
	const unsigned = viem.serializeTransaction(tx);
	const raw = await account.signTransaction(tx);
	const parsed = viem.parseTransaction(raw);
	signed.push({
		...t,
		unsigned,
		signingHash: viem.keccak256(unsigned),
		raw,
		hash: viem.keccak256(raw),
		yParity: parsed.yParity,
		r: parsed.r,
		s: parsed.s,
	});
}

// The calldata a Polymarket account needs to approve its two token contracts,
// plus the read calls this client makes through eth_call.
const calls = [
	{
		name: "approve max",
		signature: "approve(address,uint256)",
		data: viem.encodeFunctionData({
			abi: viem.parseAbi(["function approve(address spender, uint256 value)"]),
			args: ["0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E", viem.maxUint256],
		}),
	},
	{
		name: "approve zero",
		signature: "approve(address,uint256)",
		data: viem.encodeFunctionData({
			abi: viem.parseAbi(["function approve(address spender, uint256 value)"]),
			args: ["0xC5d563A36AE78145C45a50134d48A1215220f80a", 0n],
		}),
	},
	{
		name: "allowance",
		signature: "allowance(address,address)",
		data: viem.encodeFunctionData({
			abi: viem.parseAbi(["function allowance(address owner, address spender)"]),
			args: [
				"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
				"0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E",
			],
		}),
	},
	{
		name: "erc20 balanceOf",
		signature: "balanceOf(address)",
		data: viem.encodeFunctionData({
			abi: viem.parseAbi(["function balanceOf(address owner)"]),
			args: ["0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"],
		}),
	},
	{
		name: "setApprovalForAll true",
		signature: "setApprovalForAll(address,bool)",
		data: viem.encodeFunctionData({
			abi: viem.parseAbi(["function setApprovalForAll(address operator, bool approved)"]),
			args: ["0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E", true],
		}),
	},
	{
		name: "setApprovalForAll false",
		signature: "setApprovalForAll(address,bool)",
		data: viem.encodeFunctionData({
			abi: viem.parseAbi(["function setApprovalForAll(address operator, bool approved)"]),
			args: ["0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E", false],
		}),
	},
	{
		name: "isApprovedForAll",
		signature: "isApprovedForAll(address,address)",
		data: viem.encodeFunctionData({
			abi: viem.parseAbi(["function isApprovedForAll(address owner, address operator)"]),
			args: [
				"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
				"0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E",
			],
		}),
	},
	{
		name: "erc1155 balanceOf",
		signature: "balanceOf(address,uint256)",
		data: viem.encodeFunctionData({
			abi: viem.parseAbi(["function balanceOf(address owner, uint256 id)"]),
			args: [
				"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
				71321045679252212594626385532706912750332728571942532289631379312455583992563n,
			],
		}),
	},
];

// The conditional-token calls, whose arguments are dynamic where the token
// approvals' are not: an array of index sets, and the wallet batch's nested
// tuples. These are the shapes an offset can be computed wrongly in.
const CONDITION_ID = "0x1763261a2bf8884e1cfce3c83522810db637064a17cf0695846762e9b2600aa1";
const MARKET_ID = "0x9a2b3c4d5e6f70819a2b3c4d5e6f70819a2b3c4d5e6f70819a2b3c4d5e6f7081";
const CTF_ABI = viem.parseAbi([
	"function splitPosition(address collateralToken, bytes32 parentCollectionId, bytes32 conditionId, uint256[] partition, uint256 amount)",
	"function mergePositions(address collateralToken, bytes32 parentCollectionId, bytes32 conditionId, uint256[] partition, uint256 amount)",
	"function redeemPositions(address collateralToken, bytes32 parentCollectionId, bytes32 conditionId, uint256[] indexSets)",
	"function getConditionId(address oracle, bytes32 questionId, uint256 outcomeSlotCount)",
	"function getCollectionId(bytes32 parentCollectionId, bytes32 conditionId, uint256 indexSet)",
	"function getPositionId(address collateralToken, bytes32 collectionId)",
	"function payoutDenominator(bytes32 conditionId)",
]);
const NEG_RISK_ABI = viem.parseAbi([
	"function splitPosition(bytes32 conditionId, uint256 amount)",
	"function mergePositions(bytes32 conditionId, uint256 amount)",
	"function redeemPositions(bytes32 conditionId, uint256[] amounts)",
	"function convertPositions(bytes32 marketId, uint256 indexSet, uint256 amount)",
	"function getConditionId(bytes32 questionId)",
	"function getPositionId(bytes32 questionId, bool outcome)",
]);
const USDC = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174";
const ORACLE = "0x70997970C51812dc3A010C7d01b50e0d17dc79C8";
const BINARY = [1n, 2n];

function ctf(functionName, args) {
	return viem.encodeFunctionData({ abi: CTF_ABI, functionName, args });
}
function negRisk(functionName, args) {
	return viem.encodeFunctionData({ abi: NEG_RISK_ABI, functionName, args });
}

const positions = [
	{ name: "split", signature: "splitPosition(address,bytes32,bytes32,uint256[],uint256)",
	  data: ctf("splitPosition", [USDC, viem.zeroHash, CONDITION_ID, BINARY, 1000000n]) },
	{ name: "merge", signature: "mergePositions(address,bytes32,bytes32,uint256[],uint256)",
	  data: ctf("mergePositions", [USDC, viem.zeroHash, CONDITION_ID, BINARY, 250000n]) },
	{ name: "redeem", signature: "redeemPositions(address,bytes32,bytes32,uint256[])",
	  data: ctf("redeemPositions", [USDC, viem.zeroHash, CONDITION_ID, BINARY]) },
	{ name: "redeem one set", signature: "redeemPositions(address,bytes32,bytes32,uint256[])",
	  data: ctf("redeemPositions", [USDC, viem.zeroHash, CONDITION_ID, [1n]]) },
	{ name: "split five outcomes", signature: "splitPosition(address,bytes32,bytes32,uint256[],uint256)",
	  data: ctf("splitPosition", [USDC, viem.zeroHash, CONDITION_ID, [1n, 2n, 4n, 8n, 16n], 7n]) },
	{ name: "condition id", signature: "getConditionId(address,bytes32,uint256)",
	  data: ctf("getConditionId", [ORACLE, CONDITION_ID, 2n]) },
	{ name: "collection id", signature: "getCollectionId(bytes32,bytes32,uint256)",
	  data: ctf("getCollectionId", [viem.zeroHash, CONDITION_ID, 1n]) },
	{ name: "position id", signature: "getPositionId(address,bytes32)",
	  data: ctf("getPositionId", [USDC, CONDITION_ID]) },
	{ name: "payout denominator", signature: "payoutDenominator(bytes32)",
	  data: ctf("payoutDenominator", [CONDITION_ID]) },
	{ name: "neg-risk split", signature: "splitPosition(bytes32,uint256)",
	  data: negRisk("splitPosition", [CONDITION_ID, 1000000n]) },
	{ name: "neg-risk merge", signature: "mergePositions(bytes32,uint256)",
	  data: negRisk("mergePositions", [CONDITION_ID, 1000000n]) },
	{ name: "neg-risk redeem", signature: "redeemPositions(bytes32,uint256[])",
	  data: negRisk("redeemPositions", [CONDITION_ID, [1500000n, 0n]]) },
	{ name: "neg-risk convert", signature: "convertPositions(bytes32,uint256,uint256)",
	  data: negRisk("convertPositions", [MARKET_ID, 6n, 2500000n]) },
	{ name: "neg-risk condition id", signature: "getConditionId(bytes32)",
	  data: negRisk("getConditionId", [CONDITION_ID]) },
	{ name: "neg-risk position id", signature: "getPositionId(bytes32,bool)",
	  data: negRisk("getPositionId", [CONDITION_ID, true]) },
];

// The deposit wallet's batch, executed by whoever pays for it rather than by
// the relayer. Its argument is a tuple holding an array of tuples holding
// bytes, which is every nesting rule the encoder has at once.
const EXECUTE_ABI = viem.parseAbi([
	"struct Call { address target; uint256 value; bytes data; }",
	"struct Batch { address wallet; uint256 nonce; uint256 deadline; Call[] calls; }",
	"function execute(Batch batch, bytes signature)",
]);
const BATCH_WALLET = "0xD71776A8d4FdDeb3c150C4607B3f8bec31213B85";
const BATCH_SIGNATURE = "0x" + "5c".repeat(64) + "1b";
const batchCalls = [
	{ target: USDC, value: "0", data: viem.encodeFunctionData({
		abi: viem.parseAbi(["function approve(address spender, uint256 value)"]),
		args: ["0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E", viem.maxUint256] }) },
	{ target: "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045", value: "7", data: "0x" },
];
const batches = [
	{ name: "two calls", wallet: BATCH_WALLET, nonce: "56340", deadline: "1786958502", calls: batchCalls },
	{ name: "one call", wallet: BATCH_WALLET, nonce: "0", deadline: "1", calls: [batchCalls[0]] },
	{ name: "no calls", wallet: BATCH_WALLET, nonce: "18446744073709551616", deadline: "0", calls: [] },
];
for (const b of batches) {
	b.signature = BATCH_SIGNATURE;
	b.data = viem.encodeFunctionData({
		abi: EXECUTE_ABI,
		functionName: "execute",
		args: [
			{
				wallet: b.wallet,
				nonce: BigInt(b.nonce),
				deadline: BigInt(b.deadline),
				calls: b.calls.map((c) => ({ target: c.target, value: BigInt(c.value), data: c.data })),
			},
			BATCH_SIGNATURE,
		],
	});
}

// RLP items encoded on their own, so the encoder can be pinned away from a
// transaction: the empty string, a single small byte, the 55/56-byte boundary
// where the length prefix changes shape, and nested lists.
const rlp = [
	{ name: "empty string", input: "0x", encoded: viem.toRlp("0x") },
	{ name: "single zero byte", input: "0x00", encoded: viem.toRlp("0x00") },
	{ name: "single low byte", input: "0x7f", encoded: viem.toRlp("0x7f") },
	{ name: "single high byte", input: "0x80", encoded: viem.toRlp("0x80") },
	{ name: "55 bytes", input: "0x" + "61".repeat(55), encoded: viem.toRlp("0x" + "61".repeat(55)) },
	{ name: "56 bytes", input: "0x" + "61".repeat(56), encoded: viem.toRlp("0x" + "61".repeat(56)) },
	{ name: "1024 bytes", input: "0x" + "61".repeat(1024), encoded: viem.toRlp("0x" + "61".repeat(1024)) },
];

const rlpLists = [
	{ name: "empty list", items: [], encoded: viem.toRlp([]) },
	{
		name: "three strings",
		items: ["0x01", "0x", "0xdeadbeef"],
		encoded: viem.toRlp(["0x01", "0x", "0xdeadbeef"]),
	},
	{
		name: "long list",
		items: Array.from({ length: 12 }, () => "0x" + "42".repeat(8)),
		encoded: viem.toRlp(Array.from({ length: 12 }, () => "0x" + "42".repeat(8))),
	},
];

const out = {
	note: "Generated by gen-tx-vectors.mjs. Do not edit by hand.",
	privateKey,
	address: account.address,
	transactions: signed,
	calls,
	positions,
	batches,
	rlp,
	rlpLists,
};
process.stdout.write(JSON.stringify(out, null, "\t") + "\n");
