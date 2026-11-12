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

"""Unit tests for document processing pipeline and model identity checks."""

import datetime
from unittest.mock import AsyncMock, patch
import pytest
from src.drive.client import DriveFileMetadata
from src.ingestion.pipeline import _process_single_file


@pytest.mark.asyncio
async def test_process_file_reembeds_when_model_identity_differs():
    file_meta = DriveFileMetadata(
        id="file-123",
        name="Travel Policy.pdf",
        mime_type="application/pdf",
        modified_time="2026-11-10T10:00:00Z"
    )
    mtime = datetime.datetime.fromisoformat("2026-11-10T10:00:00+00:00")
    existing_doc = {
        "id": "file-123",
        "name": "Travel Policy.pdf",
        "modified_time": mtime,
        "drive_folder_id": "folder-1",
        "content_hash": "dummyhash",
        "embedding_model": "old-provider:old-model"
    }

    with patch("src.ingestion.pipeline.get_document_metadata", new_callable=AsyncMock, return_value=existing_doc), \
         patch("src.ingestion.pipeline.fetch_file_text", new_callable=AsyncMock, return_value="Travel policy text"), \
         patch("src.ingestion.pipeline.embedder.embed_texts", new_callable=AsyncMock, return_value=[[0.1] * 768]), \
         patch("src.ingestion.pipeline.upsert_document_and_chunks", new_callable=AsyncMock) as mock_upsert, \
         patch("src.config.settings.EMBEDDING_PROVIDER", "local"), \
         patch("src.config.settings.EMBEDDING_MODEL", "BAAI/bge-base-en-v1.5"):

        res = await _process_single_file(file_meta, "folder-1", "HR Policy")
        assert res == "processed"
        mock_upsert.assert_called_once()
        assert mock_upsert.call_args[1]["embedding_model"] == "local:BAAI/bge-base-en-v1.5"


@pytest.mark.asyncio
async def test_process_file_skips_when_model_and_mtime_match():
    file_meta = DriveFileMetadata(
        id="file-123",
        name="Travel Policy.pdf",
        mime_type="application/pdf",
        modified_time="2026-11-10T10:00:00Z"
    )
    mtime = datetime.datetime.fromisoformat("2026-11-10T10:00:00+00:00")
    existing_doc = {
        "id": "file-123",
        "name": "Travel Policy.pdf",
        "modified_time": mtime,
        "drive_folder_id": "folder-1",
        "content_hash": "dummyhash",
        "embedding_model": "local:BAAI/bge-base-en-v1.5"
    }

    with patch("src.ingestion.pipeline.get_document_metadata", new_callable=AsyncMock, return_value=existing_doc), \
         patch("src.ingestion.pipeline.fetch_file_text", new_callable=AsyncMock) as mock_fetch, \
         patch("src.config.settings.EMBEDDING_PROVIDER", "local"), \
         patch("src.config.settings.EMBEDDING_MODEL", "BAAI/bge-base-en-v1.5"):

        res = await _process_single_file(file_meta, "folder-1", "HR Policy")
        assert res == "unchanged"
        mock_fetch.assert_not_called()
