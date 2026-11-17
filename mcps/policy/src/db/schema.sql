-- Copyright (c) 2026 WSO2 LLC. (https://www.wso2.com).
--
-- WSO2 LLC. licenses this file to you under the Apache License,
-- Version 2.0 (the "License"); you may not use this file except
-- in compliance with the License.
-- You may obtain a copy of the License at
--
-- http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing,
-- software distributed under the License is distributed on an
-- "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
-- KIND, either express or implied.  See the License for the
-- specific language governing permissions and limitations
-- under the License.

-- Schema for Policy Knowledge Base with pgvector semantic search.

-- Enable pgvector extension (ensure latest patched version)
CREATE EXTENSION IF NOT EXISTS vector;
DO $$
BEGIN
    ALTER EXTENSION vector UPDATE;
EXCEPTION WHEN OTHERS THEN
    -- Ignore ownership or privilege errors if vector extension is managed by a cloud provider role
    NULL;
END $$;

-- Tracked Google Drive document metadata & sync timestamps
CREATE TABLE IF NOT EXISTS policy_documents (
    id              TEXT PRIMARY KEY,              -- Google Drive file ID
    name            TEXT NOT NULL,
    mime_type       TEXT NOT NULL,
    modified_time   TIMESTAMPTZ NOT NULL,
    drive_folder_id TEXT NOT NULL,                 -- Identifies HR Policy vs Training folder
    content_hash    TEXT,                          -- SHA-256 hash of extracted text
    embedding_model TEXT,                          -- Embedding provider and model identifier
    synced_at       TIMESTAMPTZ DEFAULT NOW()
);

-- Ensure embedding_model column exists on existing installations
DO $$
BEGIN
    ALTER TABLE policy_documents ADD COLUMN IF NOT EXISTS embedding_model TEXT;
EXCEPTION WHEN OTHERS THEN
    NULL;
END $$;

-- Chunked document text with vector embeddings
CREATE TABLE IF NOT EXISTS policy_chunks (
    id              BIGSERIAL PRIMARY KEY,
    document_id     TEXT NOT NULL REFERENCES policy_documents(id) ON DELETE CASCADE,
    chunk_index     INT NOT NULL,                  -- Sequential chunk position (0-indexed)
    content         TEXT NOT NULL,                 -- Chunk text returned to LLM
    embedding       VECTOR(768) NOT NULL,          -- 768-dim vector (BAAI/bge-base-en-v1.5)
    token_count     INT,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(document_id, chunk_index)
);

-- HNSW index for high-performance approximate nearest neighbor search
CREATE INDEX IF NOT EXISTS policy_chunks_embedding_hnsw
    ON policy_chunks USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

-- Auxiliary lookup indexes
CREATE INDEX IF NOT EXISTS idx_policy_docs_folder
    ON policy_documents(drive_folder_id);

CREATE INDEX IF NOT EXISTS idx_policy_chunks_doc
    ON policy_chunks(document_id);
