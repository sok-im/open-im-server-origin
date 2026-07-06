#!/usr/bin/env bash
# 转账收款通知 API 测试脚本
#
# 路由：POST /msg/notify_transfer_received
#
# 依赖：curl / jq
# 用法：
#   SENDER_USER_ID=userA RECEIVER_USER_ID=userB ./scripts/test/notify_transfer_received_api_test.sh

set -euo pipefail

HOST="${HOST:-http://13.215.203.29:10002}"
OPENIM_SECRET="${OPENIM_SECRET:-openIM123}"
ADMIN_USER_ID="${ADMIN_USER_ID:-imAdmin}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
SENDER_USER_ID="${SENDER_USER_ID:-}"
RECEIVER_USER_ID="${RECEIVER_USER_ID:-}"
BIZ_ID="${BIZ_ID:-transfer_received_$(date +%s)}"
RECEIVER_TEXT="${RECEIVER_TEXT:-你已收款}"
SENDER_TEXT="${SENDER_TEXT:-对方已收款}"
DETAIL_URL="${DETAIL_URL:-https://example.com/wallet/transfer/detail}"
OPERATION_ID="notify-transfer-received-api-test-$(date +%s)"

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

[[ -z "$SENDER_USER_ID" ]] && die "请设置 SENDER_USER_ID"
[[ -z "$RECEIVER_USER_ID" ]] && die "请设置 RECEIVER_USER_ID"
[[ -z "$ADMIN_TOKEN" ]] && ADMIN_TOKEN="$(get_admin_token)"

echo ">>> 发送转账收款通知"
RESP="$(call_api "/msg/notify_transfer_received" "$(cat <<EOF
{
  "senderUserID": "${SENDER_USER_ID}",
  "receiverUserID": "${RECEIVER_USER_ID}",
  "bizID": "${BIZ_ID}",
  "receiverText": "${RECEIVER_TEXT}",
  "senderText": "${SENDER_TEXT}",
  "detailURL": "${DETAIL_URL}"
}
EOF
)" "$ADMIN_TOKEN")"
echo "$RESP" | jq .
[[ "$(echo "$RESP" | jq -r '.errCode')" == "0" ]] || die "转账收款通知发送失败"

echo
echo "转账收款通知发送成功"
