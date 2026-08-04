"""FastAPI entry: OpenIM bot callback, health, and admin reindex."""

from __future__ import annotations

import logging
from contextlib import asynccontextmanager
from typing import Any

from fastapi import BackgroundTasks, FastAPI, Header, HTTPException, Request

from app.callback import AppState, handle_bot_message
from app.config import callback_secret_ok, get_settings
from app.idempotency import build_idempotency
from app.rag.index import get_or_create_index

logger = logging.getLogger(__name__)


@asynccontextmanager
async def lifespan(app: FastAPI):
    settings = get_settings()
    idem = build_idempotency(settings)
    index = get_or_create_index(settings)
    app.state.bot = AppState(settings=settings, idem=idem, index=index)
    logger.info("chatbot-ai ready bot_user_id=%s", settings.bot_user_id)
    yield


app = FastAPI(title="chatbot-ai", lifespan=lifespan)


@app.get("/healthz")
async def healthz() -> dict[str, bool]:
    return {"ok": True}


@app.post("/callbackAfterSendMsgToBotCommand")
async def callback_after_send_msg_to_bot(
    request: Request,
    background_tasks: BackgroundTasks,
    x_callback_secret: str | None = Header(default=None, alias="X-Callback-Secret"),
) -> dict[str, int]:
    """Quick ACK for OpenIM; RAG + send_msg run in BackgroundTasks."""
    state: AppState = request.app.state.bot
    if not callback_secret_ok(state.settings, x_callback_secret):
        raise HTTPException(status_code=401, detail="invalid callback secret")
    try:
        payload: dict[str, Any] = await request.json()
    except Exception:
        payload = {}
    if not isinstance(payload, dict):
        payload = {}
    background_tasks.add_task(handle_bot_message, payload, state)
    return {"actionCode": 0}


@app.post("/admin/reindex")
async def admin_reindex(
    request: Request,
    x_admin_token: str | None = Header(default=None, alias="X-Admin-Token"),
) -> dict[str, Any]:
    state: AppState = request.app.state.bot
    expected = state.settings.admin_reindex_token
    if not x_admin_token or x_admin_token != expected:
        raise HTTPException(status_code=401, detail="invalid admin token")
    index = get_or_create_index(state.settings, force_rebuild=True)
    state.index = index
    return {"ok": True}
