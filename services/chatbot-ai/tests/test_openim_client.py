import json

import httpx
import pytest
import respx

from app import openim_client
from app.config import Settings
from app.openim_client import ensure_admin_token, send_bot_text


@pytest.fixture(autouse=True)
def _reset_token_cache():
    openim_client.reset_admin_token_cache()
    yield
    openim_client.reset_admin_token_cache()


@pytest.fixture
def settings() -> Settings:
    return Settings(
        openim_api="http://openim.test",
        openim_admin_user="imAdmin",
        openim_admin_secret="openIM123",
        openim_admin_token="",
        bot_user_id="openIMChatbot",
        bot_nickname="在线客服",
    )


@pytest.mark.asyncio
@respx.mock
async def test_ensure_admin_token_fetches_and_caches(settings: Settings):
    route = respx.post("http://openim.test/auth/get_admin_token").mock(
        return_value=httpx.Response(
            200,
            json={
                "errCode": 0,
                "errMsg": "",
                "data": {"token": "tok-abc", "expireTimeSeconds": 7200},
            },
        )
    )
    async with httpx.AsyncClient() as client:
        t1 = await ensure_admin_token(client, settings)
        t2 = await ensure_admin_token(client, settings)

    assert t1 == "tok-abc"
    assert t2 == "tok-abc"
    assert route.call_count == 1
    assert json.loads(route.calls[0].request.content) == {
        "secret": "openIM123",
        "userID": "imAdmin",
    }


@pytest.mark.asyncio
@respx.mock
async def test_ensure_admin_token_refreshes_near_expiry(
    settings: Settings, monkeypatch: pytest.MonkeyPatch
):
    clock = {"t": 1_000.0}
    monkeypatch.setattr(openim_client.time, "time", lambda: clock["t"])

    route = respx.post("http://openim.test/auth/get_admin_token").mock(
        side_effect=[
            httpx.Response(
                200,
                json={
                    "errCode": 0,
                    "errMsg": "",
                    "data": {"token": "tok-1", "expireTimeSeconds": 120},
                },
            ),
            httpx.Response(
                200,
                json={
                    "errCode": 0,
                    "errMsg": "",
                    "data": {"token": "tok-2", "expireTimeSeconds": 7200},
                },
            ),
        ]
    )
    async with httpx.AsyncClient() as client:
        assert await ensure_admin_token(client, settings) == "tok-1"
        # expires_at=1120; with 60s skew still valid until 1060
        clock["t"] = 1_050.0
        assert await ensure_admin_token(client, settings) == "tok-1"
        assert route.call_count == 1

        clock["t"] = 1_061.0
        assert await ensure_admin_token(client, settings) == "tok-2"
        assert route.call_count == 2


@pytest.mark.asyncio
@respx.mock
async def test_ensure_admin_token_uses_configured_token(settings: Settings):
    settings = settings.model_copy(update={"openim_admin_token": "preset-token"})
    route = respx.post("http://openim.test/auth/get_admin_token").mock(
        return_value=httpx.Response(
            200,
            json={"errCode": 0, "errMsg": "", "data": {"token": "should-not-use"}},
        )
    )
    async with httpx.AsyncClient() as client:
        token = await ensure_admin_token(client, settings)

    assert token == "preset-token"
    assert route.call_count == 0


@pytest.mark.asyncio
@respx.mock
async def test_ensure_admin_token_raises_on_err_code(settings: Settings):
    respx.post("http://openim.test/auth/get_admin_token").mock(
        return_value=httpx.Response(
            200,
            json={"errCode": 1001, "errMsg": "bad secret", "data": None},
        )
    )
    async with httpx.AsyncClient() as client:
        with pytest.raises(RuntimeError, match="1001"):
            await ensure_admin_token(client, settings)


@pytest.mark.asyncio
@respx.mock
async def test_send_bot_text_success(settings: Settings):
    route = respx.post("http://openim.test/msg/send_msg").mock(
        return_value=httpx.Response(200, json={"errCode": 0, "errMsg": "", "data": {}})
    )
    async with httpx.AsyncClient() as client:
        await send_bot_text(
            client,
            settings,
            token="admin-tok",
            text="hello",
            session_type=1,
            recv_user_id="userA",
        )

    assert route.call_count == 1
    req = route.calls[0].request
    assert req.headers["token"] == "admin-tok"
    assert req.headers["operationID"]
    body = json.loads(req.content)
    assert body == {
        "recvID": "userA",
        "sendID": "openIMChatbot",
        "senderNickname": "在线客服",
        "groupID": "",
        "senderPlatformID": 0,
        "content": {"content": "hello"},
        "contentType": 101,
        "sessionType": 1,
        "isOnlineOnly": False,
        "notOfflinePush": False,
        "sendTime": 0,
    }


@pytest.mark.asyncio
@respx.mock
async def test_send_bot_text_retries_then_succeeds(settings: Settings, monkeypatch):
    sleeps: list[float] = []

    async def fake_sleep(delay: float) -> None:
        sleeps.append(delay)

    monkeypatch.setattr(openim_client.asyncio, "sleep", fake_sleep)

    route = respx.post("http://openim.test/msg/send_msg").mock(
        side_effect=[
            httpx.Response(200, json={"errCode": 500, "errMsg": "busy"}),
            httpx.Response(500, text="upstream"),
            httpx.Response(200, json={"errCode": 0, "errMsg": "", "data": {}}),
        ]
    )
    async with httpx.AsyncClient() as client:
        await send_bot_text(
            client,
            settings,
            token="t",
            text="retry-ok",
            session_type=1,
            recv_user_id="u1",
        )

    assert route.call_count == 3
    assert sleeps == [0.2, 0.5]


@pytest.mark.asyncio
@respx.mock
async def test_send_bot_text_retries_exhausted(settings: Settings, monkeypatch):
    async def fake_sleep(_delay: float) -> None:
        return None

    monkeypatch.setattr(openim_client.asyncio, "sleep", fake_sleep)

    respx.post("http://openim.test/msg/send_msg").mock(
        return_value=httpx.Response(
            200, json={"errCode": 500, "errMsg": "always fail", "data": None}
        )
    )
    async with httpx.AsyncClient() as client:
        with pytest.raises(RuntimeError, match="send_msg"):
            await send_bot_text(
                client,
                settings,
                token="t",
                text="fail",
                session_type=3,
                group_id="g1",
            )


@pytest.mark.asyncio
@respx.mock
async def test_send_bot_text_refreshes_token_on_401(
    settings: Settings, monkeypatch: pytest.MonkeyPatch
):
    async def fake_sleep(_delay: float) -> None:
        return None

    monkeypatch.setattr(openim_client.asyncio, "sleep", fake_sleep)

    # Seed cache with a stale token.
    openim_client._cached_admin_token = "stale-tok"
    openim_client._cached_admin_token_expires_at = openim_client.time.time() + 7200

    token_route = respx.post("http://openim.test/auth/get_admin_token").mock(
        return_value=httpx.Response(
            200,
            json={
                "errCode": 0,
                "errMsg": "",
                "data": {"token": "fresh-tok", "expireTimeSeconds": 7200},
            },
        )
    )
    send_route = respx.post("http://openim.test/msg/send_msg").mock(
        side_effect=[
            httpx.Response(401, text="unauthorized"),
            httpx.Response(200, json={"errCode": 0, "errMsg": "", "data": {}}),
        ]
    )
    async with httpx.AsyncClient() as client:
        await send_bot_text(
            client,
            settings,
            token="stale-tok",
            text="hi",
            session_type=1,
            recv_user_id="u1",
        )

    assert token_route.call_count == 1
    assert send_route.call_count == 2
    assert send_route.calls[1].request.headers["token"] == "fresh-tok"


@pytest.mark.asyncio
@respx.mock
async def test_send_bot_text_refreshes_token_on_errcode_1501(
    settings: Settings, monkeypatch: pytest.MonkeyPatch
):
    async def fake_sleep(_delay: float) -> None:
        return None

    monkeypatch.setattr(openim_client.asyncio, "sleep", fake_sleep)

    respx.post("http://openim.test/auth/get_admin_token").mock(
        return_value=httpx.Response(
            200,
            json={
                "errCode": 0,
                "errMsg": "",
                "data": {"token": "fresh-tok", "expireTimeSeconds": 7200},
            },
        )
    )
    send_route = respx.post("http://openim.test/msg/send_msg").mock(
        side_effect=[
            httpx.Response(
                200, json={"errCode": 1501, "errMsg": "TokenExpiredError", "data": None}
            ),
            httpx.Response(200, json={"errCode": 0, "errMsg": "", "data": {}}),
        ]
    )
    async with httpx.AsyncClient() as client:
        await send_bot_text(
            client,
            settings,
            token="old",
            text="hi",
            session_type=1,
            recv_user_id="u1",
        )

    assert send_route.call_count == 2
    assert send_route.calls[1].request.headers["token"] == "fresh-tok"
