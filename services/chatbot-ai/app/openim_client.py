"""OpenIM HTTP client: admin token + send_msg for bot replies."""

from __future__ import annotations

import asyncio
import time
import uuid
from typing import Any

import httpx

from app.config import Settings

_cached_admin_token: str | None = None
_cached_admin_token_expires_at: float | None = None  # wall-clock epoch seconds

_TOKEN_SKEW_SECONDS = 60
_DEFAULT_EXPIRE_SECONDS = 7200
# OpenIM servererrs token codes (pkg/common/servererrs/code.go)
_TOKEN_AUTH_ERR_CODES = frozenset({1501, 1502, 1503, 1504, 1505, 1506, 1507})

_RETRY_BACKOFFS = (0.2, 0.5)


def reset_admin_token_cache() -> None:
    """Clear in-process admin token cache (tests / token refresh)."""
    global _cached_admin_token, _cached_admin_token_expires_at
    _cached_admin_token = None
    _cached_admin_token_expires_at = None


def _api_base(settings: Settings) -> str:
    return settings.openim_api.rstrip("/")


def _raise_if_api_error(path: str, payload: dict[str, Any]) -> None:
    err_code = payload.get("errCode", 0)
    if err_code != 0:
        err_msg = payload.get("errMsg", "")
        raise RuntimeError(f"OpenIM {path} errCode={err_code} errMsg={err_msg}")


def _is_token_auth_failure(resp: httpx.Response) -> bool:
    """True for HTTP 401 or OpenIM token errCodes (1501–1507)."""
    if resp.status_code == 401:
        return True
    try:
        payload = resp.json()
    except Exception:
        return False
    if not isinstance(payload, dict):
        return False
    err_code = payload.get("errCode", 0)
    try:
        return int(err_code) in _TOKEN_AUTH_ERR_CODES
    except (TypeError, ValueError):
        return False


def _cache_valid(now: float) -> bool:
    if not _cached_admin_token or _cached_admin_token_expires_at is None:
        return False
    # Refresh a bit early so send_msg does not race expiry.
    return now < (_cached_admin_token_expires_at - _TOKEN_SKEW_SECONDS)


async def ensure_admin_token(client: httpx.AsyncClient, settings: Settings) -> str:
    """Return admin token: configured preset, module cache, or get_admin_token."""
    global _cached_admin_token, _cached_admin_token_expires_at

    if settings.openim_admin_token:
        return settings.openim_admin_token

    now = time.time()
    if _cache_valid(now):
        return _cached_admin_token  # type: ignore[return-value]

    path = "/auth/get_admin_token"
    url = f"{_api_base(settings)}{path}"
    resp = await client.post(
        url,
        json={
            "secret": settings.openim_admin_secret,
            "userID": settings.openim_admin_user,
        },
    )
    resp.raise_for_status()
    payload = resp.json()
    _raise_if_api_error(path, payload)
    data = payload.get("data") or {}
    token = data.get("token")
    if not token:
        raise RuntimeError(f"OpenIM {path} missing data.token")

    expire_seconds = data.get("expireTimeSeconds")
    try:
        ttl = float(expire_seconds) if expire_seconds is not None else _DEFAULT_EXPIRE_SECONDS
    except (TypeError, ValueError):
        ttl = float(_DEFAULT_EXPIRE_SECONDS)
    if ttl <= 0:
        ttl = float(_DEFAULT_EXPIRE_SECONDS)

    _cached_admin_token = token
    _cached_admin_token_expires_at = now + ttl
    return token


async def send_bot_text(
    client: httpx.AsyncClient,
    settings: Settings,
    *,
    token: str,
    text: str,
    session_type: int,
    recv_user_id: str = "",
    group_id: str = "",
) -> None:
    """POST /msg/send_msg as bot; retry up to 2 times (3 attempts) with backoff.

    On HTTP 401 / OpenIM token errCode, clear admin-token cache and retry once
    with a freshly fetched token (skipped when OPENIM_ADMIN_TOKEN is preset).
    """
    path = "/msg/send_msg"
    url = f"{_api_base(settings)}{path}"
    body = {
        "recvID": recv_user_id,
        "sendID": settings.bot_user_id,
        "senderNickname": settings.bot_nickname,
        "groupID": group_id,
        "senderPlatformID": 0,
        "content": {"content": text},
        "contentType": 101,
        "sessionType": session_type,
        "isOnlineOnly": False,
        "notOfflinePush": False,
        "sendTime": 0,
    }

    current_token = token
    auth_refreshed = False
    last_exc: Exception | None = None
    attempts = 1 + len(_RETRY_BACKOFFS)
    for attempt in range(attempts):
        try:
            resp = await client.post(
                url,
                json=body,
                headers={
                    "token": current_token,
                    "operationID": str(uuid.uuid4()),
                },
            )
            if _is_token_auth_failure(resp):
                if not auth_refreshed and not settings.openim_admin_token:
                    reset_admin_token_cache()
                    current_token = await ensure_admin_token(client, settings)
                    auth_refreshed = True
                    resp = await client.post(
                        url,
                        json=body,
                        headers={
                            "token": current_token,
                            "operationID": str(uuid.uuid4()),
                        },
                    )
                    if _is_token_auth_failure(resp):
                        raise RuntimeError(
                            f"OpenIM {path} auth failure after refresh "
                            f"HTTP {resp.status_code}: {resp.text[:200]}"
                        )
                else:
                    raise RuntimeError(
                        f"OpenIM {path} auth failure "
                        f"HTTP {resp.status_code}: {resp.text[:200]}"
                    )
            if resp.status_code != 200:
                raise RuntimeError(
                    f"OpenIM {path} HTTP {resp.status_code}: {resp.text[:200]}"
                )
            payload = resp.json()
            _raise_if_api_error(path, payload)
            return
        except Exception as exc:
            last_exc = exc
            if attempt < len(_RETRY_BACKOFFS):
                await asyncio.sleep(_RETRY_BACKOFFS[attempt])
                continue
            break

    raise RuntimeError(f"OpenIM send_msg failed after {attempts} attempts") from last_exc
