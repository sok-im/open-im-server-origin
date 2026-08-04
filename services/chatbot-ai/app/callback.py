"""OpenIM chatbot callback helpers: filter and extract user question text."""

from __future__ import annotations

import json
from typing import Any

# OpenIM protocol/constant: Text=101, AtText=106
_CONTENT_TYPE_TEXT = 101
_CONTENT_TYPE_AT_TEXT = 106
_ALLOWED_CONTENT_TYPES = {_CONTENT_TYPE_TEXT, _CONTENT_TYPE_AT_TEXT}


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
