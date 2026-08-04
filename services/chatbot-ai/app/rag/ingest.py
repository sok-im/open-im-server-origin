"""Knowledge directory → Chroma VectorStoreIndex ingest."""

from __future__ import annotations

import logging
from pathlib import Path

from llama_index.core import SimpleDirectoryReader, StorageContext, VectorStoreIndex
from llama_index.core.node_parser import SentenceSplitter

from app.config import Settings
from app.rag.index import build_embed_model, build_vector_store, collection_count

logger = logging.getLogger(__name__)

CHUNK_SIZE = 512
CHUNK_OVERLAP = 64


def ingest_knowledge(settings: Settings, *, force_rebuild: bool = True) -> int:
    """Read knowledge_dir, chunk, embed into Chroma. Returns node/chunk count."""
    knowledge_dir = Path(settings.knowledge_dir)
    if not knowledge_dir.is_dir():
        raise FileNotFoundError(f"knowledge_dir not found: {knowledge_dir}")

    documents = SimpleDirectoryReader(
        input_dir=str(knowledge_dir),
        required_exts=[".md", ".txt"],
    ).load_data()
    if not documents:
        logger.warning("no documents found under %s", knowledge_dir)
        return 0

    embed_model = build_embed_model(settings)
    vector_store = build_vector_store(settings, force_rebuild=force_rebuild)
    storage_context = StorageContext.from_defaults(vector_store=vector_store)
    splitter = SentenceSplitter(chunk_size=CHUNK_SIZE, chunk_overlap=CHUNK_OVERLAP)

    VectorStoreIndex.from_documents(
        documents,
        storage_context=storage_context,
        embed_model=embed_model,
        transformations=[splitter],
        show_progress=False,
    )

    count = collection_count(settings)
    logger.info("ingested %s nodes into chroma at %s", count, settings.chroma_path)
    return count
