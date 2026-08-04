"""Chroma-backed VectorStoreIndex load / rebuild helpers."""

from __future__ import annotations

import logging
from pathlib import Path

import chromadb
from llama_index.core import StorageContext, VectorStoreIndex
from llama_index.embeddings.openai import OpenAIEmbedding
from llama_index.llms.openai_like import OpenAILike
from llama_index.vector_stores.chroma import ChromaVectorStore

from app.config import Settings

logger = logging.getLogger(__name__)

COLLECTION_NAME = "faq"


def build_embed_model(settings: Settings) -> OpenAIEmbedding:
    return OpenAIEmbedding(
        model=settings.embed_model,
        api_base=settings.effective_embed_base_url,
        api_key=settings.effective_embed_api_key,
    )


def build_llm(settings: Settings) -> OpenAILike:
    return OpenAILike(
        model=settings.llm_model,
        api_base=settings.llm_base_url,
        api_key=settings.llm_api_key or "EMPTY",
        is_chat_model=True,
    )


def _persistent_client(settings: Settings) -> chromadb.PersistentClient:
    path = Path(settings.chroma_path)
    path.mkdir(parents=True, exist_ok=True)
    return chromadb.PersistentClient(path=str(path))


def _delete_collection(client: chromadb.PersistentClient) -> None:
    try:
        client.delete_collection(COLLECTION_NAME)
    except Exception:
        # Collection may not exist yet.
        logger.debug("chroma collection %s missing on delete", COLLECTION_NAME)


def get_chroma_collection(
    settings: Settings,
    *,
    force_rebuild: bool = False,
):
    client = _persistent_client(settings)
    if force_rebuild:
        _delete_collection(client)
    return client.get_or_create_collection(COLLECTION_NAME)


def collection_count(settings: Settings) -> int:
    client = _persistent_client(settings)
    try:
        return int(client.get_or_create_collection(COLLECTION_NAME).count())
    except Exception:
        return 0


def build_vector_store(settings: Settings, *, force_rebuild: bool = False) -> ChromaVectorStore:
    collection = get_chroma_collection(settings, force_rebuild=force_rebuild)
    return ChromaVectorStore(chroma_collection=collection)


def get_or_create_index(
    settings: Settings,
    *,
    force_rebuild: bool = False,
) -> VectorStoreIndex:
    """Load existing Chroma index; ingest knowledge when empty or force_rebuild."""
    from app.rag.ingest import ingest_knowledge

    empty = collection_count(settings) == 0
    if force_rebuild or empty:
        ingest_knowledge(settings, force_rebuild=True)

    embed_model = build_embed_model(settings)
    vector_store = build_vector_store(settings, force_rebuild=False)
    return VectorStoreIndex.from_vector_store(vector_store, embed_model=embed_model)
