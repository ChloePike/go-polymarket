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
	rlp,
	rlpLists,
};
process.stdout.write(JSON.stringify(out, null, "\t") + "\n");
