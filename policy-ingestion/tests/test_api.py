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

from unittest.mock import AsyncMock, patch
import pytest
from httpx import ASGITransport, AsyncClient
from src.config import settings
from src.main import app


@pytest.fixture(autouse=True)
def force_dummy_embedding():
    """Forces dummy embedding provider during API tests for speed and repeatability."""
    orig_provider = settings.EMBEDDING_PROVIDER
    settings.EMBEDDING_PROVIDER = "dummy"
    from src.ingestion.embedder import embedder
    embedder.provider = "dummy"
    yield
    settings.EMBEDDING_PROVIDER = orig_provider
    embedder.provider = orig_provider


@pytest.mark.asyncio
async def test_health_endpoint():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.get("/health")
        assert resp.status_code == 200
        data = resp.json()
        assert data["status"] == "ok"
        assert data["service"] == "policy-ingestion"


@pytest.mark.asyncio
async def test_ready_endpoint_connected():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        with patch("src.main.check_db_health", new_callable=AsyncMock) as mock_health:
            mock_health.return_value = True
            resp = await client.get("/ready")
            assert resp.status_code == 200
            data = resp.json()
            assert data["status"] == "ready"
            assert data["database"] == "connected"


@pytest.mark.asyncio
async def test_ready_endpoint_unavailable():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        with patch("src.main.check_db_health", new_callable=AsyncMock) as mock_health:
            mock_health.return_value = False
            resp = await client.get("/ready")
            assert resp.status_code == 503
            assert "Database connectivity unavailable" in resp.json()["detail"]


@pytest.mark.asyncio
async def test_embed_endpoint_single_text():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        payload = {"text": "How many days of sick leave are allowed?"}
        resp = await client.post("/embed", json=payload)
        assert resp.status_code == 200
        data = resp.json()
        assert "embedding" in data
        assert len(data["embedding"]) == 768
        assert data["dimension"] == 768


@pytest.mark.asyncio
async def test_embed_endpoint_batch_texts():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        payload = {"texts": ["First policy excerpt.", "Second policy excerpt."]}
        resp = await client.post("/embed", json=payload)
        assert resp.status_code == 200
        data = resp.json()
        assert "embeddings" in data
        assert len(data["embeddings"]) == 2
        assert data["count"] == 2


@pytest.mark.asyncio
async def test_embed_endpoint_missing_parameters():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post("/embed", json={})
        assert resp.status_code == 400


@pytest.mark.asyncio
async def test_admin_auth_protection():
    transport = ASGITransport(app=app)
    settings.ADMIN_API_KEY = "test-secret-key"

    try:
        async with AsyncClient(transport=transport, base_url="http://test") as client:
            # 1. Missing header -> 401 Unauthorized
            resp_unauth = await client.get("/admin/status")
            assert resp_unauth.status_code == 401

            # 2. Invalid header -> 401 Unauthorized
            resp_bad = await client.get("/admin/status", headers={"X-Admin-Key": "wrong-key"})
            assert resp_bad.status_code == 401

            # 3. Valid header -> 200 OK
            with patch("src.main.get_status_summary", new_callable=AsyncMock) as mock_summary:
                mock_summary.return_value = {
                    "total_documents": 10,
                    "total_chunks": 100,
                    "folders": [],
                    "last_synced_at": None,
                }
                resp_ok = await client.get("/admin/status", headers={"X-Admin-Key": "test-secret-key"})
                assert resp_ok.status_code == 200
                data = resp_ok.json()
                assert data["total_documents"] == 10
                assert data["total_chunks"] == 100
    finally:
        settings.ADMIN_API_KEY = None


@pytest.mark.asyncio
async def test_search_endpoint_mocked():
    transport = ASGITransport(app=app)
    mock_db_results = [
        {
            "document_name": "leave_policy.docx",
            "excerpt": "=== leave_policy.docx ===\nAnnual leave entitlement is 21 days.",
            "chunk_index": 0,
            "drive_folder_id": "folder-123",
            "similarity_score": 0.8842,
        }
    ]

    async with AsyncClient(transport=transport, base_url="http://test") as client:
        with patch("src.main.search_similar_chunks", new_callable=AsyncMock) as mock_search:
            mock_search.return_value = mock_db_results
            payload = {"query": "annual leave days", "top_k": 3}
            resp = await client.post("/search", json=payload)
            assert resp.status_code == 200
            data = resp.json()
            assert data["query"] == "annual leave days"
            assert data["results_count"] == 1
            assert len(data["results"]) == 1
            assert data["results"][0]["document_name"] == "leave_policy.docx"
