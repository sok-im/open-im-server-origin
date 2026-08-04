"""Retriever + LLM answer with fallback strings from settings."""

from __future__ import annotations

import logging
from typing import Optional

from llama_index.core import VectorStoreIndex
from llama_index.core.query_engine import RetrieverQueryEngine
from llama_index.core.response_synthesizers import get_response_synthesizer
from llama_index.core.prompts import PromptTemplate

from app.config import Settings
from app.rag.index import build_llm

logger = logging.getLogger(__name__)

SYSTEM_PROMPT_TEMPLATE = (
    "你是在线客服助手。只依据检索到的资料回答用户问题；"
    "若资料中没有足够依据，直接回复「{fallback_no_hit}」，不要编造。"
)

QA_PROMPT = PromptTemplate(
    "系统约束：{system_prompt}\n\n"
    "参考资料：\n"
    "---------------------\n"
    "{context_str}\n"
    "---------------------\n"
    "用户问题：{query_str}\n"
    "请用中文简洁回答："
)


def compose_fallback(
    retrieved_empty: bool,
    llm_error: bool,
    settings: Settings,
) -> Optional[str]:
    """Return a fallback reply string, or None when the caller should use the LLM answer."""
    if retrieved_empty:
        return settings.fallback_no_hit
    if llm_error:
        return settings.fallback_busy
    return None


def answer_question(settings: Settings, index: VectorStoreIndex, question: str) -> str:
    """Retrieve top-k chunks and synthesize an answer; map empty/error to fallbacks."""
    question = (question or "").strip()
    if not question:
        return settings.fallback_no_hit

    retriever = index.as_retriever(similarity_top_k=settings.similarity_top_k)
    try:
        nodes = retriever.retrieve(question)
    except Exception:
        logger.exception("retrieve failed")
        return settings.fallback_busy

    fallback = compose_fallback(retrieved_empty=not nodes, llm_error=False, settings=settings)
    if fallback is not None:
        return fallback

    llm = build_llm(settings)
    system_prompt = SYSTEM_PROMPT_TEMPLATE.format(fallback_no_hit=settings.fallback_no_hit)
    synthesizer = get_response_synthesizer(
        llm=llm,
        text_qa_template=QA_PROMPT.partial_format(system_prompt=system_prompt),
    )
    query_engine = RetrieverQueryEngine(retriever=retriever, response_synthesizer=synthesizer)
    try:
        response = query_engine.query(question)
    except Exception:
        logger.exception("llm synthesize failed")
        return compose_fallback(False, True, settings) or settings.fallback_busy

    text = str(response).strip()
    return text or settings.fallback_no_hit
