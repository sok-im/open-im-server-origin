#!/usr/bin/env bash
# 支付通知 API 测试脚本
#
# 路由：POST /msg/send_payment_notification
#
# 依赖：curl / jq
# 用法：
#   RECV_USER_ID=user123 ./scripts/test/payment_notification_api_test.sh

set -euo pipefail

HOST="${HOST:-http://127.0.0.1:10002}"
OPENIM_SECRET="${OPENIM_SECRET:-openIM123}"
ADMIN_USER_ID="${ADMIN_USER_ID:-imAdmin}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
RECV_USER_ID="${RECV_USER_ID:-}"
PAYMENT_SEND_USER_ID="${PAYMENT_SEND_USER_ID:-payment_notification_bot}"
OPERATION_ID="payment-notify-api-test-$(date +%s)"

die() { echo "ERROR: $*" >&2; exit 1; }

get_admin_token() {
  local uid body resp token
  for uid in "${ADMIN_USER_ID}" "imAdmin" "openIM123456"; do
    body="{\"secret\":\"${OPENIM_SECRET}\",\"userID\":\"${uid}\"}"
    resp="$(curl -sS -X POST "${HOST}/auth/get_admin_token" \
      -H "Content-Type: application/json" \
      -H "operationID: ${OPERATION_ID}" \
      -d "$body")"
    token="$(echo "$resp" | jq -r '.data.token // empty')"
    [[ -n "$token" ]] && { echo "$token"; return 0; }
  done
  die "获取管理员 token 失败"
}

call_api() {
  local path="$1" body="$2" token="$3"
  curl -sS -X POST "${HOST}${path}" \
    -H "Content-Type: application/json" \
    -H "operationID: ${OPERATION_ID}" \
    -H "token: ${token}" \
    -d "$body"
}

[[ -z "$RECV_USER_ID" ]] && die "请设置 RECV_USER_ID"
[[ -z "$ADMIN_TOKEN" ]] && ADMIN_TOKEN="$(get_admin_token)"

echo ">>> 发送支付通知"
PAYMENT_RESP="$(call_api "/msg/send_payment_notification" "$(cat <<EOF
{
  "sendUserID": "${PAYMENT_SEND_USER_ID}",
  "recvUserID": "${RECV_USER_ID}",
  "content": {
    "title": "红包/转账过期",
    "amount": "-136.00",
    "transactionType": "红包/转账",
    "transactionTime": "2026-06-18 17:04:25",
    "currency": "USDT",
    "currencyIconURL": "https://example.com/icons/usdt.png",
    "detailURL": "https://example.com/wallet/expired/detail",
    "detailText": "查看详情",
    "secondaryAction": {
      "text": "去赎回",
      "url": "https://example.com/wallet/redeem"
    },
    "orderNo": "ORD$(date +%s)",
    "bizID": "rp_transfer_001"
  }
}
EOF
)" "$ADMIN_TOKEN")"
echo "$PAYMENT_RESP" | jq .
[[ "$(echo "$PAYMENT_RESP" | jq -r '.errCode')" == "0" ]] || die "支付通知发送失败"

echo
echo "支付通知发送成功"
