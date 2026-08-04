"""Optional X-Callback-Secret gate (unit-level; no FastAPI TestClient)."""

from app.config import Settings, callback_secret_ok


def test_callback_secret_ok_when_unset():
    settings = Settings(callback_secret="")
    assert callback_secret_ok(settings, None) is True
    assert callback_secret_ok(settings, "anything") is True


def test_callback_secret_requires_match_when_set():
    settings = Settings(callback_secret="s3cr3t")
    assert callback_secret_ok(settings, None) is False
    assert callback_secret_ok(settings, "wrong") is False
    assert callback_secret_ok(settings, "s3cr3t") is True
