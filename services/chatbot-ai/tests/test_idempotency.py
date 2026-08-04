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


def test_confirm_survives_short_ttl():
    """After confirm, sleeping past short_ttl must still block re-acquire."""
    s = MemoryIdempotencyStore(short_ttl=1, long_ttl=86400)
    assert s.try_acquire("m2b") is True
    s.confirm("m2b")
    time.sleep(1.1)
    assert s.try_acquire("m2b") is False


def test_confirm_after_short_ttl_expired():
    """acquire → wait short expire → confirm → still blocked (rewrite long TTL)."""
    s = MemoryIdempotencyStore(short_ttl=1, long_ttl=86400)
    assert s.try_acquire("m2c") is True
    time.sleep(1.1)
    s.confirm("m2c")
    assert s.try_acquire("m2c") is False


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
