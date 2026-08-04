from app.config import Settings
from app.rag.query import compose_fallback


def test_compose_fallback_empty_retrieve():
    s = Settings(fallback_no_hit="NO_HIT", fallback_busy="BUSY")
    assert compose_fallback(True, False, s) == "NO_HIT"
    assert compose_fallback(True, True, s) == "NO_HIT"  # empty wins


def test_compose_fallback_llm_error():
    s = Settings(fallback_no_hit="NO_HIT", fallback_busy="BUSY")
    assert compose_fallback(False, True, s) == "BUSY"


def test_compose_fallback_ok_is_none():
    s = Settings(fallback_no_hit="NO_HIT", fallback_busy="BUSY")
    assert compose_fallback(False, False, s) is None
