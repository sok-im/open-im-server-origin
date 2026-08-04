"""Session validation before RAG and handle_bot_message early returns."""

from __future__ import annotations

from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from app.callback import AppState, handle_bot_message, resolve_session_target
from app.config import Settings
from app.idempotency import MemoryIdempotencyStore


def test_resolve_session_single_ok():
    assert resolve_session_target({"sessionType": 1, "sendID": "u1"}) == (1, "u1", "")


def test_resolve_session_group_ok():
    assert resolve_session_target({"sessionType": 3, "groupID": "g1"}) == (3, "", "g1")


def test_resolve_session_rejects_bad_type_or_missing_fields():
    assert resolve_session_target({"sessionType": 2, "sendID": "u1"}) is None
    assert resolve_session_target({"sessionType": 1, "sendID": ""}) is None
    assert resolve_session_target({"sessionType": 1}) is None
    assert resolve_session_target({"sessionType": 3, "groupID": ""}) is None
    assert resolve_session_target({"sessionType": 3}) is None


@pytest.mark.asyncio
async def test_handle_bot_message_skips_rag_on_bad_session():
    settings = Settings(openim_admin_token="t")
    idem = MemoryIdempotencyStore()
    state = AppState(settings=settings, idem=idem, index=MagicMock())
    payload = {
        "clientMsgID": "cid-bad-session",
        "contentType": 101,
        "content": '{"content":"hello"}',
        "sessionType": 2,
        "sendID": "u1",
    }
    with patch("app.callback.answer_question") as answer:
        await handle_bot_message(payload, state)
        answer.assert_not_called()
    # Short lock held, no confirm — same id still blocked until TTL.
    assert idem.try_acquire("cid-bad-session") is False


@pytest.mark.asyncio
async def test_handle_bot_message_uses_to_thread_for_rag():
    settings = Settings(openim_admin_token="preset")
    idem = MemoryIdempotencyStore()
    state = AppState(settings=settings, idem=idem, index=MagicMock())
    payload = {
        "clientMsgID": "cid-ok",
        "contentType": 101,
        "content": '{"content":"hello"}',
        "sessionType": 1,
        "sendID": "u1",
    }
    with (
        patch("app.callback.asyncio.to_thread", new_callable=AsyncMock) as to_thread,
        patch("app.callback.send_bot_text", new_callable=AsyncMock) as send,
        patch("app.callback.ensure_admin_token", new_callable=AsyncMock) as ensure,
        patch("app.callback.httpx.AsyncClient") as client_cls,
    ):
        to_thread.return_value = "answer"
        ensure.return_value = "preset"
        client_cls.return_value.__aenter__ = AsyncMock(return_value=MagicMock())
        client_cls.return_value.__aexit__ = AsyncMock(return_value=None)

        await handle_bot_message(payload, state)

        to_thread.assert_awaited_once()
        send.assert_awaited_once()
        assert idem.try_acquire("cid-ok") is False  # confirmed → still locked
