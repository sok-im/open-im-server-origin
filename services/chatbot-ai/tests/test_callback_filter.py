"""Unit tests for OpenIM chatbot callback text extraction."""

from app.callback import parse_user_question


def test_parse_text_json_content():
    payload = {
        "contentType": 101,
        "content": '{"content":"如何重置密码？"}',
    }
    assert parse_user_question(payload) == "如何重置密码？"


def test_parse_plain_text_content():
    payload = {
        "contentType": 101,
        "content": "纯文本问题",
    }
    assert parse_user_question(payload) == "纯文本问题"


def test_parse_empty_content_returns_none():
    assert parse_user_question({"contentType": 101, "content": ""}) is None
    assert parse_user_question({"contentType": 101, "content": '{"content":""}'}) is None
    assert parse_user_question({"contentType": 101, "content": '{"content":"   "}'}) is None
    assert parse_user_question({"contentType": 101}) is None


def test_parse_wrong_content_type_returns_none():
    payload = {
        "contentType": 102,  # Picture
        "content": '{"content":"should ignore"}',
    }
    assert parse_user_question(payload) is None


def test_parse_at_text_with_content():
    payload = {
        "contentType": 106,  # AtText
        "content": '{"content":"@bot 营业时间？"}',
    }
    assert parse_user_question(payload) == "@bot 营业时间？"


def test_parse_at_text_with_text_field():
    """OpenIM AtElem uses `text`, not `content`."""
    payload = {
        "contentType": 106,
        "content": '{"text":"@bot 你好","atUserList":["openIMChatbot"],"isAtSelf":false}',
    }
    assert parse_user_question(payload) == "@bot 你好"
