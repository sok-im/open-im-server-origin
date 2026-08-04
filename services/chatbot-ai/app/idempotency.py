"""clientMsgID idempotency: short acquire TTL, confirm extends to long TTL."""

from __future__ import annotations

import time
from typing import Protocol

from app.config import Settings


class IdempotencyStore(Protocol):
    def try_acquire(self, client_msg_id: str) -> bool: ...

    def confirm(self, client_msg_id: str) -> None: ...


class MemoryIdempotencyStore:
    """In-process dict store keyed by client_msg_id with expiry timestamps."""

    def __init__(self, short_ttl: float = 300, long_ttl: float = 86400) -> None:
        self._short_ttl = short_ttl
        self._long_ttl = long_ttl
        self._entries: dict[str, float] = {}

    def _purge_expired(self, now: float) -> None:
        expired = [k for k, exp in self._entries.items() if exp <= now]
        for k in expired:
            del self._entries[k]

    def try_acquire(self, client_msg_id: str) -> bool:
        now = time.monotonic()
        self._purge_expired(now)
        if client_msg_id in self._entries:
            return False
        self._entries[client_msg_id] = now + self._short_ttl
        return True

    def confirm(self, client_msg_id: str) -> None:
        """Always rewrite long TTL (even if short TTL already expired / purged)."""
        now = time.monotonic()
        self._purge_expired(now)
        # Spec: 或重写 — unconditional; do not no-op after purge.
        self._entries[client_msg_id] = now + self._long_ttl


def build_idempotency(settings: Settings) -> IdempotencyStore:
    backend = (settings.idempotency_backend or "memory").lower()
    if backend == "memory":
        return MemoryIdempotencyStore()
    if backend == "redis":
        raise NotImplementedError(
            "redis idempotency backend not wired yet; use idempotency_backend=memory"
        )
    raise ValueError(f"unsupported idempotency_backend: {settings.idempotency_backend!r}")
