# RedPacket 接口变更说明

## 背景

这次红包改造的目标，是在保持前端改动尽量小的前提下，让红包业务支持多条链运行：

- `ETH`
- `BSC`
- `TRON`

同时兼容“测试链先跑”的场景，例如：

- `eth-sepolia`
- `bsc-testnet`
- `tron-nile`

本次改造的核心思路是把链标识拆成两层：

- `chainType`: 链类型，主要用于钱包签名体系区分，只保留 `EVM` / `TRON`
- `chainKey`: 具体链实例，主要用于红包业务路由，例如 `eth-sepolia`、`bsc-testnet`

这样做的结果是：

- 钱包绑定仍然可以按 `chainType` 复用
- 红包创建、领取、退款、事件解析等业务，按 `chainKey` 精确路由
- 前端切换链时，尽量只切配置，不改业务代码

## 变更结论

本次接口改造的重点不是大改接口结构，而是：

1. 红包业务接口统一新增 `chainKey`
2. 钱包绑定接口增加可选的 `chainKey` 上下文
3. 部分返回结构增加 `chainKey`，便于前端感知当前记录属于哪条链
4. `chainType` 仍然保留，不需要前端立刻删掉

前端最小改造建议：

- 原来已经传 `chainType` 的地方继续传
- 红包业务接口统一补传 `chainKey`
- 钱包绑定接口可选补传 `chainKey`
- 前端环境配置统一维护：
  - `chainKey`
  - `chainType`
  - `chainID`
  - `contractAddress`

## 接口字段变更明细

下面按接口维度说明本次改动。

### 1. 创建红包

接口：

- `CreateOrderReq`

新增字段：

- `chainKey`

当前请求字段：

- `chainType`
- `chainID`
- `contractAddress`
- `creatorWallet`
- `groupID`
- `scopeType`
- `receiverUserID`
- `receiverUserIDs`
- `packetType`
- `token`
- `totalAmount`
- `totalShares`
- `expiryAt`
- `remark`
- `chainKey`

改造原因：

- 原来只靠 `chainType` 无法区分 `ETH` 和 `BSC` 这类同属 `EVM` 的不同链实例
- 增加 `chainKey` 后，后端才能明确选择正确的 RPC、合约实例和运行时配置

前端要求：

- 必传 `chainKey`
- 保留传 `chainType`

推荐请求示例：

```json
{
  "chainKey": "eth-sepolia",
  "chainType": "EVM",
  "chainID": 11155111,
  "contractAddress": "0xYourProxyAddress"
}
```

### 2. 红包创建回调

接口：

- `CreatedCallbackReq`

新增字段：

- `chainKey`

改造原因：

- 创建成功回调需要明确是在哪条链创建出来的红包
- 避免同一个 `packetID` / 同一个钱包在不同 EVM 测试链里出现混淆

### 3. 红包详情

接口：

- `GetDetailReq`

新增字段：

- `chainKey`

说明：

- `packetID` + `chainKey` 才能稳定定位一条红包记录
- `chainType` 仍保留用于兼容和校验

### 4. 领取签名

接口：

- `IssueClaimSignReq`

新增字段：

- `chainKey`

改造原因：

- 同一个 EVM 钱包地址可以同时出现在 `ETH`、`BSC` 多条链
- 领取签名必须绑定具体链，否则可能出现跨链串单

### 5. 提交领取结果

接口：

- `ClaimResultReq`

新增字段：

- `chainKey`

改造原因：

- 领取结果上报时，需要精确落到正确链的红包记录和领取记录

### 6. 申请退款

接口：

- `RequestRefundReq`

新增字段：

- `chainKey`

### 7. 查询退款结果

接口：

- `GetRefundReq`

新增字段：

- `chainKey`

返回新增字段：

- `GetRefundResp.chainKey`

改造原因：

- 退款动作和退款记录也必须按具体链区分

### 8. 钱包绑定挑战

接口：

- `IssueWalletBindChallengeReq`

新增字段：

- `chainKey`

返回新增字段：

- `IssueWalletBindChallengeResp.chainKey`

说明：

- 钱包绑定本身仍然主要按 `chainType` 归类
- 这里补 `chainKey` 的目的，是把当前请求的链上下文记下来，方便挑战记录和后续排查

### 9. 钱包绑定确认

接口：

- `ConfirmWalletBindResp`

返回新增字段：

- `chainKey`

说明：

- 返回 `chainKey` 主要是为了让前端知道这次挑战来自哪条链上下文
- 不代表钱包绑定关系改成按链隔离

### 10. 查询钱包绑定

接口：

- `GetWalletBindingReq`

新增字段：

- `chainKey`

返回新增字段：

- `GetWalletBindingResp.chainKey`

说明：

- 查询绑定关系仍然以 `chainType + walletAddress` 为主
- `chainKey` 更多是上下文信息，不是主键升级

### 11. 管理后台配置接口

以下管理接口新增 `chainKey`：

- `SetSignerReq.chainKey`
- `SetTokenReq.chainKey`
- `SetExpiryReq.chainKey`
- `SetAllowAllTokensReq.chainKey`
- `SetNativeTokenEnabledReq.chainKey`
- `ParseTxEventsReq.chainKey`

改造原因：

- 管理接口必须明确操作的是哪一条链上的合约
- 否则同一个后端实例挂多条 EVM / TRON 链时会产生误操作风险

## 返回结构变更

除了请求补字段，这次还有几类返回结构新增了 `chainKey`。

### RedPacketRecord

新增：

- `chainKey`

影响：

- 前端获取红包详情后，可以直接知道该红包属于哪条链
- 对多链环境下的详情展示、调试排查更友好

### GetRefundResp

新增：

- `chainKey`

### 钱包绑定相关返回

新增：

- `IssueWalletBindChallengeResp.chainKey`
- `ConfirmWalletBindResp.chainKey`
- `GetWalletBindingResp.chainKey`

## 这次没有改的部分

为了做到“切链主要改配置”，这次有几件事是刻意没改重的。

### 1. `chainType` 没删

原因：

- 老逻辑和前端已有参数还能继续用
- 方便平滑迁移
- 也保留了对签名体系的明确区分

### 2. 钱包绑定没有改成按链实例绑定

原因：

- 你的业务判断是对的：`EVM` 钱包地址可以跨 `ETH` / `BSC` 复用
- 所以绑定关系仍然按：
  - `chainType + walletAddress`

而不是：

- `chainKey + walletAddress`

### 3. 前端不需要针对每个链写一套新接口

原因：

- 这次只是在原接口上补 `chainKey`
- 前端最好通过统一拦截器、请求构造器或者环境配置注入该字段

## 前端最小改造方案

建议前端统一维护一个红包链配置对象，例如：

```ts
export const redpacketChain = {
  chainKey: "bsc-testnet",
  chainType: "EVM",
  chainID: 97,
  contractAddress: "0xYourProxyAddress",
};
```

然后对红包业务接口统一补：

```json
{
  "chainKey": "bsc-testnet",
  "chainType": "EVM"
}
```

最小改造落点建议：

1. 新增统一链配置
2. 红包业务请求自动注入 `chainKey`
3. 钱包绑定请求继续传 `chainType`，可选补 `chainKey`
4. 页面业务逻辑不写死具体链名

## 接口改动总表

### 请求新增 `chainKey`

- `CreateOrderReq`
- `CreatedCallbackReq`
- `GetDetailReq`
- `IssueClaimSignReq`
- `ClaimResultReq`
- `IssueWalletBindChallengeReq`
- `GetWalletBindingReq`
- `RequestRefundReq`
- `GetRefundReq`
- `SetSignerReq`
- `SetTokenReq`
- `SetExpiryReq`
- `SetAllowAllTokensReq`
- `SetNativeTokenEnabledReq`
- `ParseTxEventsReq`

### 返回新增 `chainKey`

- `RedPacketRecord`
- `IssueWalletBindChallengeResp`
- `ConfirmWalletBindResp`
- `GetWalletBindingResp`
- `GetRefundResp`

## 为什么这样改

这次改造选择“保留 `chainType`，新增 `chainKey`”，而不是直接把所有逻辑替换成 `chainKey`，主要有三个原因：

1. 前端改动最小

- 现有 `chainType` 逻辑可以保留
- 新增一个配置字段即可支持多链

2. 钱包绑定更符合业务现实

- `EVM` 钱包天然可跨多条 EVM 链复用
- 没必要把同一个地址拆成多个绑定关系

3. 后端路由更准确

- 红包创建、领取、退款必须精确到链实例
- `chainKey` 能明确指向 `eth-sepolia` / `bsc-testnet` / `tron-nile`

## 相关文档

- 多链接入说明：
  [chainkey-integration-guide.md](/Users/panda/aiCode/red_packet/open-im-server-origin/cmd/openim-rpc/openim-rpc-redpacket/chainkey-integration-guide.md)
- 协议定义：
  [redpacket.proto](/Users/panda/aiCode/red_packet/open-im-server-origin/protocol/redpacket/redpacket.proto)
