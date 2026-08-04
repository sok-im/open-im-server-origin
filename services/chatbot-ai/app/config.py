from functools import lru_cache

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    openim_api: str = "http://127.0.0.1:10002"
    openim_admin_user: str = "imAdmin"
    openim_admin_secret: str = "openIM123"
    openim_admin_token: str = ""  # 非空则跳过登录
    bot_user_id: str = "openIMChatbot"
    bot_nickname: str = "在线客服"

    llm_base_url: str = "https://api.openai.com/v1"
    llm_api_key: str = ""
    llm_model: str = "gpt-4o-mini"

    embed_base_url: str = ""
    embed_api_key: str = ""
    embed_model: str = "text-embedding-3-small"

    chroma_path: str = "./data/chroma"
    knowledge_dir: str = "./knowledge"
    similarity_top_k: int = 4
    admin_reindex_token: str = "change-me"
    idempotency_backend: str = "memory"  # memory | redis
    redis_url: str = "redis://127.0.0.1:6379/0"

    fallback_no_hit: str = "暂未查到相关说明，请换个问法或联系人工客服。"
    fallback_busy: str = "客服繁忙，请稍后再试。"

    @property
    def effective_embed_base_url(self) -> str:
        return self.embed_base_url or self.llm_base_url

    @property
    def effective_embed_api_key(self) -> str:
        return self.embed_api_key or self.llm_api_key


@lru_cache
def get_settings() -> Settings:
    return Settings()
