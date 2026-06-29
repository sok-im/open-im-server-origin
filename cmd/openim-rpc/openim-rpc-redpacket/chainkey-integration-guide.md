# RedPacket Multi-Chain Integration Guide

This document describes the backend changes for `chainKey`-based redpacket routing
and the minimum frontend changes required to integrate with it.

## What Changed

The redpacket service now separates:

- `chainType`: wallet/signature family, still only `EVM` or `TRON`
- `chainKey`: concrete runtime instance such as `eth-sepolia`, `bsc-testnet`, `tron-nile`

Wallet binding remains reusable by `chainType`:

- one EVM wallet binding can be reused across ETH/BSC EVM networks
- TRON binding remains TRON-specific

Redpacket business flows are now routed by `chainKey`:

- create order
- created callback
- claim sign
- claim result
- refund
- tx event parsing

## Backend Data Rules

The following collections now persist `chain_key`:

- `red_packet`
- `red_packet_claim`
- `red_packet_claim_auth`
- `red_packet_refund`
- `wallet_binding_challenge`

`wallet_binding` intentionally stays reusable by `chain_type + wallet_address`.

## Frontend Changes

Frontend should keep one environment-level chain config:

```ts
export const redpacketChain = {
  chainKey: "bsc-testnet",
  chainType: "EVM",
  chainID: 97,
  contractAddress: "0x9f2e22F5D0cf8d8127E319D38b3EDDaE43bb4DC0",
};
```

### 1. Required fields for redpacket business APIs

Pass `chainKey` on these requests:

- `CreateOrder`
- `CreatedCallback`
- `GetDetail`
- `IssueClaimSign`
- `ClaimResult`
- `RequestRefund`
- `GetRefund`
- admin endpoints if used

Keep sending `chainType` for compatibility and validation.

Recommended request shape:

```json
{
  "chainKey": "bsc-testnet",
  "chainType": "EVM"
}
```

### 2. Wallet binding APIs

Wallet binding still keys off `chainType`.

Recommended behavior:

- EVM wallet bind: send `chainType=EVM`
- TRON wallet bind: send `chainType=TRON`
- optional: also send `chainKey` to preserve the runtime context for challenge records

Example challenge request:

```json
{
  "chainKey": "eth-sepolia",
  "chainType": "EVM",
  "chainID": 11155111,
  "walletAddress": "0x1234..."
}
```

### 3. Minimal environment switching rule

To switch test networks or move to mainnet, frontend should only update config:

- `chainKey`
- `chainType`
- `chainID`
- `contractAddress`

Business code should not hardcode chain names.

## API Notes

### Create order

If `chainKey` is present, backend resolves:

- runtime RPC
- contract address fallback
- signer/admin runtime

If there are multiple EVM runtimes and the request omits `chainKey`, backend now
returns an error instead of guessing.

### Claim sign / claim result

Claim flow always follows the `chainKey` stored on the redpacket record. This
prevents ETH/BSC mixed processing when the same wallet address is reused across
multiple EVM networks.

### Refund

Refund lookup and refund execution now also use `chainKey`.

## Suggested Frontend Rollout

1. Add a single redpacket chain config object per environment.
2. Update the request builder/interceptor to append `chainKey` for redpacket APIs.
3. Keep wallet binding UI unchanged except for sending the new optional `chainKey`.
4. Verify:
   - EVM wallet binds once and can create packets on both ETH/BSC testnets
   - claim/refund stay isolated per `chainKey`
