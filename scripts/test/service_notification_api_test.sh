#!/usr/bin/env bash
# 服务通知 API 测试脚本
#
# 路由：POST /msg/batch_send_service_notification
#
# 依赖：curl / jq
# 用法：
#   ./scripts/test/service_notification_api_test.sh
#   RECV_USER_ID=user123 ./scripts/test/service_notification_api_test.sh
#   IS_SEND_ALL=true ./scripts/test/service_notification_api_test.sh   # 全员发送

set -euo pipefail

HOST="${HOST:-http://127.0.0.1:10002}"
OPENIM_SECRET="${OPENIM_SECRET:-openIM123}"
ADMIN_USER_ID="${ADMIN_USER_ID:-imAdmin}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
RECV_USER_ID="${RECV_USER_ID:-}"
IS_SEND_ALL="${IS_SEND_ALL:-false}"
SERVICE_SEND_USER_ID="${SERVICE_SEND_USER_ID:-service_notification_bot}"
OPERATION_ID="service-notify-api-test-$(date +%s)"

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

[[ -z "$ADMIN_TOKEN" ]] && ADMIN_TOKEN="$(get_admin_token)"
if [[ "$IS_SEND_ALL" != "true" && -z "$RECV_USER_ID" ]]; then
  die "请设置 RECV_USER_ID，或设置 IS_SEND_ALL=true 全员发送"
fi

if [[ "$IS_SEND_ALL" == "true" ]]; then
  SERVICE_BATCH_BODY="$(cat <<EOF
{
  "sendUserID": "${SERVICE_SEND_USER_ID}",
  "isSendAll": true,
  "content": {
    "title": "版本更新",
    "content": "《SOK V2.0》版本已经正式发布，本次更新优化了聊天体验并修复已知问题，欢迎更新体验。",
    "subType": 4,
    "detailURL": "https://example.com/release/v2.0",
    "detailText": "查看详情"
  }
}
EOF
)"
else
  SERVICE_BATCH_BODY="$(cat <<EOF
{
  "sendUserID": "${SERVICE_SEND_USER_ID}",
  "isSendAll": false,
  "recvIDs": ["${RECV_USER_ID}"],
  "content": {
    "title": "版本更新",
    "content": "《SOK V2.0》版本已经正式发布，本次更新优化了聊天体验并修复已知问题，欢迎更新体验。",
    "subType": 4,
    "detailURL": "https://example.com/release/v2.0",
    "detailText": "查看详情"
  }
}
EOF
)"
fi

echo ">>> 批量发送服务通知"
SERVICE_RESP="$(call_api "/msg/batch_send_service_notification" "$SERVICE_BATCH_BODY" "$ADMIN_TOKEN")"
echo "$SERVICE_RESP" | jq .
[[ "$(echo "$SERVICE_RESP" | jq -r '.errCode')" == "0" ]] || die "服务通知发送失败"

echo
echo "服务通知发送成功"
