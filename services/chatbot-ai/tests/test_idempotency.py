import time

from app.idempotency import MemoryIdempotencyStore, build_idempotency
from app.config import Settings


def test_acquire_once():
    s = MemoryIdempotencyStore(short_ttl=60, long_ttl=86400)
    assert s.try_acquire("m1") is True
    assert s.try_acquire("m1") is False


def test_confirm_keeps_block():
    s = MemoryIdempotencyStore(short_ttl=1, long_ttl=86400)
    assert s.try_acquire("m2") is True
    s.confirm("m2")
    assert s.try_acquire("m2") is False


def test_short_ttl_expires():
    s = MemoryIdempotencyStore(short_ttl=1, long_ttl=86400)
    assert s.try_acquire("m3") is True
    time.sleep(1.1)
    assert s.try_acquire("m3") is True


def test_build_idempotency_memory():
    settings = Settings(idempotency_backend="memory")
    store = build_idempotency(settings)
    assert isinstance(store, MemoryIdempotencyStore)
    assert store.try_acquire("factory-1") is True
    assert store.try_acquire("factory-1") is False
