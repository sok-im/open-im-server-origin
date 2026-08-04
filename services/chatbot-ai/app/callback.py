"""OpenIM chatbot callback helpers: filter, extract question, and process pipeline."""

from __future__ import annotations

import asyncio
import json
import logging
from typing import Any

import httpx
from llama_index.core import VectorStoreIndex

from app.config import Settings
from app.idempotency import IdempotencyStore
from app.openim_client import ensure_admin_token, send_bot_text
from app.rag.query import answer_question

logger = logging.getLogger(__name__)

# OpenIM protocol/constant: Text=101, AtText=106
_CONTENT_TYPE_TEXT = 101
_CONTENT_TYPE_AT_TEXT = 106
_ALLOWED_CONTENT_TYPES = {_CONTENT_TYPE_TEXT, _CONTENT_TYPE_AT_TEXT}

_SESSION_TYPE_SINGLE = 1
_SESSION_TYPE_GROUP = 3


class AppState:
    """Shared runtime state for the FastAPI app (settings, idempotency, index)."""

    def __init__(
        self,
        settings: Settings,
        idem: IdempotencyStore,
        index: VectorStoreIndex,
    ) -> None:
        self.settings = settings
        self.idem = idem
        self.index = index


def parse_user_question(payload: dict) -> str | None:
    """Extract user question from OpenIM after-send-to-bot callback payload.

    Returns None when contentType is not Text/AtText, or when content is empty.
    `content` may be a JSON string ``{"content":"..."}`` / AtElem ``{"text":"..."}``
    or plain text.
    """
    content_type = payload.get("contentType")
    if content_type not in _ALLOWED_CONTENT_TYPES:
        return None

    raw = payload.get("content")
    if raw is None:
        return None
    if not isinstance(raw, str):
        raw = str(raw)

    text = _extract_text(raw)
    if text is None:
        return None
    text = text.strip()
    return text or None


def _extract_text(raw: str) -> str | None:
    stripped = raw.strip()
    if not stripped:
        return None

    try:
        parsed: Any = json.loads(stripped)
    except (json.JSONDecodeError, TypeError):
        return stripped

    if isinstance(parsed, dict):
        for key in ("content", "text"):
            value = parsed.get(key)
            if isinstance(value, str):
                return value
        return None
    if isinstance(parsed, str):
        return parsed
    return None


def resolve_session_target(payload: dict) -> tuple[int, str, str] | None:
    """Return (sessionType, recv_user_id, group_id) when payload is sendable.

    Single chat (1) requires sendID (reply target). Group (3) requires groupID.
    Other sessionType values are unsupported.
    """
    try:
        st = int(payload.get("sessionType") or 0)
    except (TypeError, ValueError):
        return None
    if st == _SESSION_TYPE_SINGLE:
        recv = str(payload.get("sendID") or "").strip()
        if not recv:
            return None
        return st, recv, ""
    if st == _SESSION_TYPE_GROUP:
        gid = str(payload.get("groupID") or "").strip()
        if not gid:
            return None
        return st, "", gid
    return None


async def handle_bot_message(payload: dict, app_state: AppState) -> None:
    """Acquire idempotency, RAG answer, send bot reply, then confirm long TTL."""
    cid = payload.get("clientMsgID") or ""
    if not cid or not app_state.idem.try_acquire(cid):
        return
    try:
        q = parse_user_question(payload)
        if not q:
            return

        # Validate session before RAG. Return without confirm so the short
        # idempotency lock expires and OpenIM can retry with a fixed payload.
        target = resolve_session_target(payload)
        if target is None:
            logger.warning(
                "unsupported or incomplete session clientMsgID=%s sessionType=%s",
                cid,
                payload.get("sessionType"),
            )
            return

        st, recv_user_id, group_id = target
        text = await asyncio.to_thread(
            answer_question, app_state.settings, app_state.index, q
        )
        async with httpx.AsyncClient(timeout=30) as client:
            token = await ensure_admin_token(client, app_state.settings)
            await send_bot_text(
                client,
                app_state.settings,
                token=token,
                text=text,
                session_type=st,
                recv_user_id=recv_user_id,
                group_id=group_id,
            )
        app_state.idem.confirm(cid)
    except Exception:
        logger.exception("bot pipeline failed clientMsgID=%s", cid)
