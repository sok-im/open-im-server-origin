#!/usr/bin/env bash
set -euo pipefail

# 初始化专属通知账号（服务通知 / 支付通知）
#
# 用法：
#   ./scripts/init_notification_accounts.sh            # 创建默认账号（已存在则跳过）
#   ./scripts/init_notification_accounts.sh --update # 已存在时更新昵称/头像
#   ./scripts/init_notification_accounts.sh --list     # 列出所有通知账号
#
# 环境变量（可覆盖）：
#   OPENIM_API_ADDR                 默认: http://127.0.0.1:10002
#   ADMIN_TOKEN                     管理员 token（未设置则自动获取）
#   OPENIM_SECRET                   默认: openIM123
#   ADMIN_USER_ID                   默认: imAdmin
#   SERVICE_NOTIFICATION_USER_ID    默认: service_notification_bot
#   SERVICE_NOTIFICATION_NICKNAME     默认: 服务通知
#   SERVICE_NOTIFICATION_FACE_URL     默认: 空
#   PAYMENT_NOTIFICATION_USER_ID      默认: payment_notification_bot
#   PAYMENT_NOTIFICATION_NICKNAME     默认: 支付通知
#   PAYMENT_NOTIFICATION_FACE_URL       默认: 空
#   APP_MANAGER_LEVEL                 默认: 3 (AppNotificationAdmin)

OPENIM_API_ADDR="${OPENIM_API_ADDR:-http://127.0.0.1:10002}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
OPENIM_SECRET="${OPENIM_SECRET:-openIM123}"
ADMIN_USER_ID="${ADMIN_USER_ID:-imAdmin}"
OPERATION_ID="${OPERATION_ID:-init_notify_$(date +%s)_$RANDOM}"

SERVICE_NOTIFICATION_USER_ID="${SERVICE_NOTIFICATION_USER_ID:-service_notification_bot}"
SERVICE_NOTIFICATION_NICKNAME="${SERVICE_NOTIFICATION_NICKNAME:-服务通知}"
SERVICE_NOTIFICATION_FACE_URL="${SERVICE_NOTIFICATION_FACE_URL:-}"

PAYMENT_NOTIFICATION_USER_ID="${PAYMENT_NOTIFICATION_USER_ID:-payment_notification_bot}"
PAYMENT_NOTIFICATION_NICKNAME="${PAYMENT_NOTIFICATION_NICKNAME:-支付通知}"
PAYMENT_NOTIFICATION_FACE_URL="${PAYMENT_NOTIFICATION_FACE_URL:-}"

APP_MANAGER_LEVEL="${APP_MANAGER_LEVEL:-3}"
LAST_HTTP_CODE=""
LAST_API_RESP=""

ACTION="init"
UPDATE_EXISTING=0

die() {
  echo "ERROR: $*" >&2
  exit 1
}

info() {
  echo "INFO: $*"
}

ok() {
  echo "OK:   $*"
}

warn() {
  echo "WARN: $*" >&2
}

usage() {
  cat <<'EOF'
用法:
  ./scripts/init_notification_accounts.sh [--update]     初始化服务通知/支付通知账号
  ./scripts/init_notification_accounts.sh --list         列出所有通知账号

选项:
  --update   账号已存在时更新昵称和头像
  --list     仅查询，不创建
  -h, --help 显示帮助
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --update)
        UPDATE_EXISTING=1
        shift
        ;;
      --list)
        ACTION="list"
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "未知参数: $1"
        ;;
    esac
  done
}

json_get() {
  local json="$1"
  local path="$2"
  python3 - <<'PY' "$json" "$path"
import json
import sys

raw, path = sys.argv[1], sys.argv[2]
try:
    obj = json.loads(raw)
except Exception:
    print("")
    raise SystemExit(0)

cur = obj
for part in path.split("."):
    if not isinstance(cur, dict):
        print("")
        raise SystemExit(0)
    cur = cur.get(part)
    if cur is None:
        print("")
        raise SystemExit(0)

if isinstance(cur, bool):
    print("true" if cur else "false")
elif cur is None:
    print("")
else:
    print(cur)
PY
}

get_admin_token() {
  local uid body resp token last_resp
  local -a candidates=("${ADMIN_USER_ID}" "openIM123456" "imAdmin")
  last_resp=""

  for uid in "${candidates[@]}"; do
    body="{\"secret\":\"${OPENIM_SECRET}\",\"userID\":\"${uid}\"}"
    resp="$(curl -sS -X POST "${OPENIM_API_ADDR}/auth/get_admin_token" \
      -H "Content-Type: application/json" \
      -H "operationID: ${OPERATION_ID}" \
      -d "$body")"
    last_resp="$resp"

    token="$(json_get "$resp" "data.token")"
    if [[ -z "$token" ]]; then
      token="$(json_get "$resp" "token")"
    fi
    if [[ -n "$token" ]]; then
      info "自动获取管理员 token 成功，userID=${uid}"
      printf '%s' "$token"
      return 0
    fi
  done

  echo "get_admin_token raw response: $last_resp" >&2
  die "自动获取管理员 token 失败，请检查 OPENIM_API_ADDR/OPENIM_SECRET/ADMIN_USER_ID，或直接设置 ADMIN_TOKEN"
}

call_api() {
  local path="$1"
  local body="$2"
  local token="$3"
  local resp http_code

  resp="$(curl -sS -w $'\n__HTTP_CODE__:%{http_code}' -X POST "${OPENIM_API_ADDR}${path}" \
    -H "Content-Type: application/json" \
    -H "operationID: ${OPERATION_ID}" \
    -H "token: ${token}" \
    -d "$body")"

  http_code="${resp##*__HTTP_CODE__:}"
  resp="${resp%$'\n'__HTTP_CODE__:*}"
  LAST_HTTP_CODE="$http_code"
  LAST_API_RESP="$resp"
  printf '%s' "$resp"
}

format_api_error() {
  local path="$1"
  local resp="${2:-$LAST_API_RESP}"
  local http_code="${3:-$LAST_HTTP_CODE}"

  if [[ -z "$resp" ]]; then
    echo "HTTP ${http_code:-unknown}, empty response (path=${path})"
    return 0
  fi

  if [[ "$(json_get "$resp" "errCode")" != "" ]]; then
    echo "HTTP ${http_code:-unknown}, path=${path}, body=${resp}"
    return 0
  fi

  echo "HTTP ${http_code:-unknown}, path=${path}, body=${resp} (not OpenIM JSON; rebuild/restart openim-api and openim-rpc-user after pulling latest code)"
}

ensure_admin_token() {
  if [[ -z "$ADMIN_TOKEN" ]]; then
    info "ADMIN_TOKEN 未设置，尝试自动获取管理员 token..."
    ADMIN_TOKEN="$(get_admin_token)"
  fi
}

create_notification_account() {
  local user_id="$1"
  local nick_name="$2"
  local face_url="$3"
  local token="$4"
  local body resp err_code err_msg

  body="$(python3 - <<'PY' "$user_id" "$nick_name" "$face_url" "$APP_MANAGER_LEVEL"
import json
import sys

user_id, nick_name, face_url, app_level = sys.argv[1:5]
payload = {
    "userID": user_id,
    "nickName": nick_name,
    "faceURL": face_url,
    "appMangerLevel": int(app_level),
}
print(json.dumps(payload, ensure_ascii=False))
PY
)"

  info "创建通知账号 userID=${user_id}, nickName=${nick_name}"
  resp="$(call_api "/user/add_notification_account" "$body" "$token")"
  err_code="$(json_get "$resp" "errCode")"
  err_msg="$(json_get "$resp" "errMsg")"

  if [[ "$err_code" == "0" ]]; then
    ok "创建成功 userID=${user_id}"
    echo "$resp"
    return 0
  fi

  if [[ "$err_msg" == *"userID is used"* ]]; then
    warn "账号已存在 userID=${user_id}"
    return 1
  fi

  die "创建通知账号失败 userID=${user_id}: $(format_api_error "/user/add_notification_account" "$resp")"
}

update_notification_account() {
  local user_id="$1"
  local nick_name="$2"
  local face_url="$3"
  local token="$4"
  local body resp err_code

  body="$(python3 - <<'PY' "$user_id" "$nick_name" "$face_url"
import json
import sys

user_id, nick_name, face_url = sys.argv[1:4]
payload = {"userID": user_id, "nickName": nick_name}
if face_url:
    payload["faceURL"] = face_url
print(json.dumps(payload, ensure_ascii=False))
PY
)"

  info "更新通知账号 userID=${user_id}, nickName=${nick_name}"
  resp="$(call_api "/user/update_notification_account" "$body" "$token")"
  err_code="$(json_get "$resp" "errCode")"

  if [[ "$err_code" == "0" ]]; then
    ok "更新成功 userID=${user_id}"
    echo "$resp"
    return 0
  fi

  die "更新通知账号失败 userID=${user_id}: $(format_api_error "/user/update_notification_account" "$resp")"
}

init_account() {
  local user_id="$1"
  local nick_name="$2"
  local face_url="$3"
  local token="$4"

  # 直接尝试创建；账号已存在时 create 返回 1，避免依赖 search 接口做存在性探测
  if create_notification_account "$user_id" "$nick_name" "$face_url" "$token" >/dev/null; then
    return 0
  fi

  if [[ "$UPDATE_EXISTING" -eq 1 ]]; then
    update_notification_account "$user_id" "$nick_name" "$face_url" "$token" >/dev/null
  else
    ok "已存在，跳过 userID=${user_id}"
  fi
}

list_notification_accounts() {
  local token="$1"
  local resp err_code

  resp="$(call_api "/user/search_notification_account" \
    '{"pagination":{"pageNumber":1,"showNumber":100}}' \
    "$token")"
  err_code="$(json_get "$resp" "errCode")"

  if [[ "$err_code" != "0" ]]; then
    die "查询通知账号列表失败: $(format_api_error "/user/search_notification_account" "$resp")"
  fi

  echo "$resp" | python3 - <<'PY'
import json
import sys

raw = sys.stdin.read().strip()
try:
    obj = json.loads(raw)
except Exception:
    print(raw)
    raise SystemExit(0)

accounts = (((obj or {}).get("data") or {}).get("notificationAccounts")) or []
total = (((obj or {}).get("data") or {}).get("total")) or 0

print(f"通知账号总数: {total}")
for item in accounts:
    print(
        f"- userID={item.get('userID','')}, "
        f"nickName={item.get('nickName','')}, "
        f"appMangerLevel={item.get('appMangerLevel','')}, "
        f"faceURL={item.get('faceURL','')}"
    )
PY
}

main() {
  parse_args "$@"
  ensure_admin_token

  case "$ACTION" in
    list)
      info "查询通知账号列表..."
      list_notification_accounts "$ADMIN_TOKEN"
      ;;
    init)
      info "开始初始化专属通知账号..."
      info "API=${OPENIM_API_ADDR}"
      init_account "$SERVICE_NOTIFICATION_USER_ID" "$SERVICE_NOTIFICATION_NICKNAME" "$SERVICE_NOTIFICATION_FACE_URL" "$ADMIN_TOKEN"
      init_account "$PAYMENT_NOTIFICATION_USER_ID" "$PAYMENT_NOTIFICATION_NICKNAME" "$PAYMENT_NOTIFICATION_FACE_URL" "$ADMIN_TOKEN"
      echo
      info "当前通知账号："
      list_notification_accounts "$ADMIN_TOKEN"
      ;;
    *)
      die "不支持的 action: ${ACTION}"
      ;;
  esac
}

main "$@"
