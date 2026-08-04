from unittest.mock import patch

from app.config import Settings
from app.rag.ingest import ingest_knowledge


def test_ingest_empty_dir_force_rebuild_deletes_collection(tmp_path):
    knowledge = tmp_path / "knowledge"
    knowledge.mkdir()
    settings = Settings(
        knowledge_dir=str(knowledge),
        chroma_path=str(tmp_path / "chroma"),
    )

    with patch("app.rag.ingest.build_vector_store") as mock_bvs:
        count = ingest_knowledge(settings, force_rebuild=True)

    assert count == 0
    mock_bvs.assert_called_once_with(settings, force_rebuild=True)


def test_ingest_empty_dir_without_force_skips_delete(tmp_path):
    knowledge = tmp_path / "knowledge"
    knowledge.mkdir()
    settings = Settings(
        knowledge_dir=str(knowledge),
        chroma_path=str(tmp_path / "chroma"),
    )

    with patch("app.rag.ingest.build_vector_store") as mock_bvs:
        count = ingest_knowledge(settings, force_rebuild=False)

    assert count == 0
    mock_bvs.assert_not_called()
