#!/usr/bin/env bash
# 红包领取通知 API 测试脚本
#
# 路由：POST /msg/notify_red_packet_claimed
#
# 依赖：curl / jq
# 用法：
#   SENDER_USER_ID=userA RECEIVER_USER_ID=userB ./scripts/test/notify_red_packet_claimed_api_test.sh

set -euo pipefail

HOST="${HOST:-http://127.0.0.1:10002}"
OPENIM_SECRET="${OPENIM_SECRET:-openIM123}"
ADMIN_USER_ID="${ADMIN_USER_ID:-imAdmin}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
SENDER_USER_ID="${SENDER_USER_ID:-}"
RECEIVER_USER_ID="${RECEIVER_USER_ID:-}"
BIZ_ID="${BIZ_ID:-rp_claim_$(date +%s)}"
RECEIVER_TEXT="${RECEIVER_TEXT:-你领取了对方的红包}"
SENDER_TEXT="${SENDER_TEXT:-对方领取了你的红包}"
DETAIL_URL="${DETAIL_URL:-https://example.com/wallet/redpacket/detail}"
OPERATION_ID="notify-red-packet-claimed-api-test-$(date +%s)"

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

echo ">>> 发送红包领取通知"
RESP="$(call_api "/msg/notify_red_packet_claimed" "$(cat <<EOF
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
[[ "$(echo "$RESP" | jq -r '.errCode')" == "0" ]] || die "红包领取通知发送失败"

echo
echo "红包领取通知发送成功"
