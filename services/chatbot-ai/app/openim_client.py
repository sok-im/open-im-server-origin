"""OpenIM HTTP client: admin token + send_msg for bot replies."""

from __future__ import annotations

import asyncio
import uuid
from typing import Any

import httpx

from app.config import Settings

_cached_admin_token: str | None = None

_RETRY_BACKOFFS = (0.2, 0.5)


def reset_admin_token_cache() -> None:
    """Clear in-process admin token cache (tests / token refresh)."""
    global _cached_admin_token
    _cached_admin_token = None


def _api_base(settings: Settings) -> str:
    return settings.openim_api.rstrip("/")


def _raise_if_api_error(path: str, payload: dict[str, Any]) -> None:
    err_code = payload.get("errCode", 0)
    if err_code != 0:
        err_msg = payload.get("errMsg", "")
        raise RuntimeError(f"OpenIM {path} errCode={err_code} errMsg={err_msg}")


async def ensure_admin_token(client: httpx.AsyncClient, settings: Settings) -> str:
    """Return admin token: configured preset, module cache, or get_admin_token."""
    global _cached_admin_token

    if settings.openim_admin_token:
        return settings.openim_admin_token
    if _cached_admin_token:
        return _cached_admin_token

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
    _cached_admin_token = token
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
    """POST /msg/send_msg as bot; retry up to 2 times (3 attempts) with backoff."""
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

    last_exc: Exception | None = None
    attempts = 1 + len(_RETRY_BACKOFFS)
    for attempt in range(attempts):
        try:
            resp = await client.post(
                url,
                json=body,
                headers={
                    "token": token,
                    "operationID": str(uuid.uuid4()),
                },
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
