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

# Database client for PostgreSQL with pgvector operations.

import logging
import os
from typing import Any, Dict, List, Optional
import asyncpg
from pgvector.asyncpg import register_vector

from ..config import settings

logger = logging.getLogger("policy_ingestion.db")

_pool: Optional[asyncpg.Pool] = None


async def get_db_pool() -> asyncpg.Pool:
    """Returns or creates the global asyncpg connection pool."""
    global _pool
    if _pool is None:
        dsn = settings.get_database_dsn()
        logger.info("Initializing PostgreSQL connection pool...")

        async def init_connection(conn: asyncpg.Connection):
            # Register pgvector codec if extension is active
            try:
                await register_vector(conn)
            except Exception as e:
                logger.debug(f"pgvector registration check: {e}")

        # Extract sslmode if present for asyncpg
        ssl = "require" if settings.DB_SSL.lower() in ("true", "require", "1") else False
        _pool = await asyncpg.create_pool(
            dsn=dsn,
            min_size=2,
            max_size=10,
            init=init_connection,
            timeout=30.0,
        )
        logger.info("PostgreSQL connection pool initialized successfully.")
    return _pool


async def close_db_pool() -> None:
    """Closes the asyncpg connection pool."""
    global _pool
    if _pool is not None:
        logger.info("Closing PostgreSQL connection pool...")
        await _pool.close()
        _pool = None


async def init_db() -> None:
    """Initializes tables and extensions from schema.sql."""
    pool = await get_db_pool()
    schema_path = os.path.join(os.path.dirname(__file__), "schema.sql")
    if not os.path.exists(schema_path):
        logger.warning(f"Schema file not found at {schema_path}")
        return

    with open(schema_path, "r", encoding="utf-8") as f:
        schema_sql = f.read()

    async with pool.acquire() as conn:
        logger.info("Executing schema.sql migrations...")
        await conn.execute(schema_sql)
        logger.info("Database schema initialized successfully.")


async def check_db_health() -> bool:
    """Verifies that the database is reachable and responsive."""
    try:
        pool = await get_db_pool()
        async with pool.acquire() as conn:
            val = await conn.fetchval("SELECT 1")
            return val == 1
    except Exception as e:
        logger.error(f"Database health check failed: {e}")
        return False


async def get_document_metadata(doc_id: str) -> Optional[Dict[str, Any]]:
    """Retrieves metadata of a single tracked policy document."""
    pool = await get_db_pool()
    query = """
        SELECT id, name, mime_type, modified_time, drive_folder_id, content_hash, synced_at
        FROM policy_documents
        WHERE id = $1
    """
    async with pool.acquire() as conn:
        row = await conn.fetchrow(query, doc_id)
        return dict(row) if row else None


async def upsert_document_and_chunks(
    doc_id: str,
    name: str,
    mime_type: str,
    modified_time: Any,
    drive_folder_id: str,
    content_hash: str,
    chunks: List[Dict[str, Any]]
) -> None:
    """
    Atomic transaction: upserts document metadata and replaces its chunk embeddings.
    """
    pool = await get_db_pool()
    async with pool.acquire() as conn:
        async with conn.transaction():
            # 1. Upsert document record
            await conn.execute(
                """
                INSERT INTO policy_documents (
                    id, name, mime_type, modified_time, drive_folder_id, content_hash, synced_at
                )
                VALUES ($1, $2, $3, $4, $5, $6, NOW())
                ON CONFLICT (id) DO UPDATE SET
                    name = EXCLUDED.name,
                    mime_type = EXCLUDED.mime_type,
                    modified_time = EXCLUDED.modified_time,
                    drive_folder_id = EXCLUDED.drive_folder_id,
                    content_hash = EXCLUDED.content_hash,
                    synced_at = NOW();
                """,
                doc_id, name, mime_type, modified_time, drive_folder_id, content_hash
            )

            # 2. Delete prior chunks for this document
            await conn.execute("DELETE FROM policy_chunks WHERE document_id = $1;", doc_id)

            # 3. Insert fresh chunks with embeddings
            chunk_records = [
                (
                    doc_id,
                    c["chunk_index"],
                    c["content"],
                    c["embedding"],
                    c.get("token_count", 0)
                )
                for c in chunks
            ]
            if chunk_records:
                await conn.executemany(
                    """
                    INSERT INTO policy_chunks (document_id, chunk_index, content, embedding, token_count)
                    VALUES ($1, $2, $3, $4, $5);
                    """,
                    chunk_records
                )


async def prune_missing_documents(drive_folder_id: str, active_doc_ids: List[str]) -> int:
    """Removes documents (and cascading chunks) that no longer exist in the Drive folder."""
    if not active_doc_ids:
        return 0
    pool = await get_db_pool()
    async with pool.acquire() as conn:
        deleted = await conn.execute(
            """
            DELETE FROM policy_documents
            WHERE drive_folder_id = $1 AND NOT (id = ANY($2::text[]));
            """,
            drive_folder_id,
            active_doc_ids,
        )
        try:
            return int(deleted.split()[-1])
        except Exception:
            return 0


async def get_status_summary() -> Dict[str, Any]:
    """Returns total counts of documents and chunks per folder."""
    pool = await get_db_pool()
    async with pool.acquire() as conn:
        doc_count = await conn.fetchval("SELECT COUNT(*) FROM policy_documents;")
        chunk_count = await conn.fetchval("SELECT COUNT(*) FROM policy_chunks;")
        folder_stats = await conn.fetch(
            """
            SELECT drive_folder_id, COUNT(*) as doc_count
            FROM policy_documents
            GROUP BY drive_folder_id;
            """
        )
        last_sync = await conn.fetchval("SELECT MAX(synced_at) FROM policy_documents;")

        return {
            "total_documents": doc_count,
            "total_chunks": chunk_count,
            "folders": [dict(r) for r in folder_stats],
            "last_synced_at": last_sync.isoformat() if last_sync else None
        }


async def search_similar_chunks(
    embedding: List[float],
    drive_folder_id: Optional[str] = None,
    top_k: int = 5
) -> List[Dict[str, Any]]:
    """Performs cosine distance similarity search on policy chunks."""
    pool = await get_db_pool()
    # Format embedding as vector string representation
    vector_literal = f"[{','.join(f'{x:.8f}' for x in embedding)}]"

    base_query = """
        SELECT
            pd.name AS document_name,
            pc.content AS excerpt,
            pc.chunk_index,
            pd.drive_folder_id,
            1 - (pc.embedding <=> $1::vector) AS similarity_score
        FROM policy_chunks pc
        JOIN policy_documents pd ON pd.id = pc.document_id
    """
    params = [vector_literal]

    if drive_folder_id:
        base_query += " WHERE pd.drive_folder_id = $2"
        params.append(drive_folder_id)
        base_query += " ORDER BY pc.embedding <=> $1::vector LIMIT $3;"
        params.append(top_k)
    else:
        base_query += " ORDER BY pc.embedding <=> $1::vector LIMIT $2;"
        params.append(top_k)

    async with pool.acquire() as conn:
        rows = await conn.fetch(base_query, *params)
        return [dict(r) for r in rows]
