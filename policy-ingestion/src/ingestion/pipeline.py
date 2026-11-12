# Copyright (c) 2026 WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

# Ingestion pipeline: Google Drive polling, change detection, chunking, and pgvector upsert.

import asyncio
import datetime
import hashlib
import logging
import time
from typing import Any, Dict, List

from ..config import settings
from ..db.client import (
    get_document_metadata,
    prune_missing_documents,
    upsert_document_and_chunks,
)
from ..drive.client import DriveFileMetadata, drive_client
from ..drive.extractor import fetch_file_text
from .chunker import chunker
from .embedder import embedder

logger = logging.getLogger("policy_ingestion.pipeline")

_sync_lock = asyncio.Lock()


async def sync_drive_folder(folder_id: str, label: str = "Folder") -> Dict[str, Any]:
    """
    Synchronizes a single Google Drive folder into the pgvector database.
    Performs change detection based on modifiedTime and SHA-256 content hashes.
    """
    if not folder_id:
        logger.warning(f"[{label}] Drive folder ID not configured, skipping sync.")
        return {"status": "skipped", "reason": "empty_folder_id"}

    start_time = time.time()
    logger.info(f"[{label}] Starting Drive synchronization for folder: {folder_id}")

    try:
        files = await drive_client.list_folder_files(folder_id)
    except Exception as e:
        logger.error(f"[{label}] Failed to list Drive folder files: {e}")
        return {"status": "error", "error": str(e)}

    stats = {
        "folder_label": label,
        "folder_id": folder_id,
        "total_files_found": len(files),
        "documents_processed": 0,
        "documents_unchanged": 0,
        "documents_skipped": 0,
        "chunks_created": 0,
    }

    for file_meta in files:
        try:
            doc_status = await _process_single_file(file_meta, folder_id, label)
            if doc_status == "processed":
                stats["documents_processed"] += 1
            elif doc_status == "unchanged":
                stats["documents_unchanged"] += 1
            else:
                stats["documents_skipped"] += 1
        except Exception as e:
            logger.error(f"[{label}] Error processing file '{file_meta.name}' ({file_meta.id}): {e}")
            stats["documents_skipped"] += 1

    # Prune documents removed from Google Drive
    active_doc_ids = [f.id for f in files]
    pruned_count = await prune_missing_documents(folder_id, active_doc_ids)
    stats["documents_pruned"] = pruned_count
    if pruned_count > 0:
        logger.info(f"[{label}] Pruned {pruned_count} deleted documents from vector database.")

    stats["elapsed_seconds"] = round(time.time() - start_time, 2)
    logger.info(
        f"[{label}] Sync finished: {stats['documents_processed']} updated, "
        f"{stats['documents_unchanged']} unchanged, {stats['documents_skipped']} skipped, "
        f"{stats['documents_pruned']} pruned in {stats['elapsed_seconds']}s."
    )
    return stats


async def _process_single_file(
    file_meta: DriveFileMetadata,
    folder_id: str,
    label: str
) -> str:
    """Processes an individual file from Drive, returning 'processed', 'unchanged', or 'skipped'."""
    existing_doc = await get_document_metadata(file_meta.id)

    # Convert ISO modifiedTime string to datetime object for comparison
    drive_mtime = datetime.datetime.fromisoformat(file_meta.modified_time.replace("Z", "+00:00"))

    if existing_doc and existing_doc.get("modified_time"):
        db_mtime = existing_doc["modified_time"]
        # If DB mtime is equal to or newer than Drive mtime, document hasn't changed
        if db_mtime >= drive_mtime:
            logger.debug(f"[{label}] Document '{file_meta.name}' unchanged (mtime match).")
            return "unchanged"

    # Extract text from supported formats
    text = await fetch_file_text(file_meta)
    if not text or not text.strip():
        logger.debug(f"[{label}] Skipped '{file_meta.name}' (no extractable text).")
        return "skipped"

    content_hash = hashlib.sha256(text.encode("utf-8")).hexdigest()

    # Fast hash comparison if file was touched without text modifications
    if existing_doc and existing_doc.get("content_hash") == content_hash:
        logger.debug(f"[{label}] Document '{file_meta.name}' unchanged (content hash match).")
        return "unchanged"

    # Chunk the extracted text
    text_chunks = chunker.chunk_document(file_meta.id, file_meta.name, text)
    if not text_chunks:
        return "skipped"

    logger.info(
        f"[{label}] Embedding {len(text_chunks)} chunks for '{file_meta.name}'..."
    )

    # Embed chunks in batches of 20
    chunk_data: List[Dict[str, Any]] = []
    batch_size = 20
    for i in range(0, len(text_chunks), batch_size):
        batch = text_chunks[i:i + batch_size]
        texts_to_embed = [c.content for c in batch]
        embeddings = await embedder.embed_texts(texts_to_embed)

        for c, emb in zip(batch, embeddings):
            chunk_data.append({
                "chunk_index": c.chunk_index,
                "content": c.content,
                "embedding": emb,
                "token_count": c.token_count,
            })

    # Persist metadata and chunks atomically
    await upsert_document_and_chunks(
        doc_id=file_meta.id,
        name=file_meta.name,
        mime_type=file_meta.mime_type,
        modified_time=drive_mtime,
        drive_folder_id=folder_id,
        content_hash=content_hash,
        chunks=chunk_data,
    )

    return "processed"


async def run_full_sync() -> Dict[str, Any]:
    """
    Executes a complete sync across all configured Google Drive folders.
    Protected by an async lock against overlapping concurrent runs.
    """
    if _sync_lock.locked():
        logger.warning("Synchronization run already in progress. Skipping trigger.")
        return {"status": "in_progress", "message": "Sync currently active"}

    async with _sync_lock:
        start_overall = time.time()
        results: Dict[str, Any] = {"folders": []}

        # 1. Sync HR Policy Folder
        if settings.policy_folder_id:
            res_policy = await sync_drive_folder(
                settings.policy_folder_id,
                label="HR_POLICY"
            )
            results["folders"].append(res_policy)
        else:
            logger.info("POLICY_DRIVE_FOLDER_ID not set. Skipping HR policy sync.")

        # 2. Sync Training Playbook Folder
        if settings.training_folder_id:
            res_training = await sync_drive_folder(
                settings.training_folder_id,
                label="TRAINING"
            )
            results["folders"].append(res_training)
        else:
            logger.info("TRAINING_DRIVE_FOLDER_ID not set. Skipping training sync.")

        results["total_elapsed_seconds"] = round(time.time() - start_overall, 2)
        results["status"] = "completed"
        return results
