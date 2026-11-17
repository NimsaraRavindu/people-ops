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

"""Unit tests for the unified Policy MCP Server endpoints and background sync lifecycle."""

import asyncio
import base64
import json
import time
from unittest.mock import AsyncMock, patch
import pytest
from httpx import ASGITransport, AsyncClient
from src.config import settings
from src.main import app


def _make_test_jwt(email: str = "nimsaraa@wso2.com", exp_offset: int = 3600) -> str:
    """Creates a valid 3-part test JWT with RS256 header for dev-mode identity extraction."""
    header = {"alg": "RS256", "typ": "JWT", "kid": "test-kid"}
    payload = {
        "sub": "user-123",
        "email": email,
        "exp": int(time.time()) + exp_offset,
    }

    def _b64(data: dict) -> str:
        raw = json.dumps(data).encode("utf-8")
        return base64.urlsafe_b64encode(raw).decode("ascii").rstrip("=")

    return f"{_b64(header)}.{_b64(payload)}.fakesignature"


@pytest.fixture(autouse=True)
def force_dummy_embedding():
    """Forces dummy embedding provider during API tests for speed and repeatability."""
    orig_provider = settings.EMBEDDING_PROVIDER
    settings.EMBEDDING_PROVIDER = "dummy"
    from src.ingestion.embedder import embedder

    embedder.provider = "dummy"
    embedder.cache.clear()
    yield
    settings.EMBEDDING_PROVIDER = orig_provider
    embedder.provider = orig_provider
    embedder.cache.clear()


@pytest.mark.asyncio
async def test_health_endpoint_ok():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        with patch("src.main.check_db_health", new_callable=AsyncMock, return_value=True):
            resp = await client.get("/health")
            assert resp.status_code == 200
            data = resp.json()
            assert data["status"] == "ok"
            assert data["service"] == "policy-mcp"
            assert data["database"] == "connected"


@pytest.mark.asyncio
async def test_health_endpoint_degraded():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        with patch("src.main.check_db_health", new_callable=AsyncMock, return_value=False):
            resp = await client.get("/health")
            assert resp.status_code == 503
            data = resp.json()
            assert data["status"] == "degraded"
            assert data["service"] == "policy-mcp"
            assert data["database"] == "unreachable"


@pytest.mark.asyncio
async def test_ready_endpoint_connected():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        with patch("src.main._db_initialized", True), patch(
            "src.main.check_db_health", new_callable=AsyncMock
        ) as mock_health:
            mock_health.return_value = True
            resp = await client.get("/ready")
            assert resp.status_code == 200
            data = resp.json()
            assert data["status"] == "ready"
            assert data["database"] == "connected"


@pytest.mark.asyncio
async def test_ready_endpoint_db_not_initialized():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        with patch("src.main._db_initialized", False), patch(
            "src.main.init_db", side_effect=RuntimeError("connection refused")
        ):
            resp = await client.get("/ready")
            assert resp.status_code == 503
            assert "Database schema initialization failed" in resp.json()["detail"]


@pytest.mark.asyncio
async def test_ready_endpoint_recovers_after_db_initialized():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        with patch("src.main._db_initialized", False), patch(
            "src.main.init_db", new_callable=AsyncMock
        ) as mock_init, patch(
            "src.main.check_db_health", new_callable=AsyncMock
        ) as mock_health:
            mock_init.return_value = None
            mock_health.return_value = True
            resp = await client.get("/ready")
            assert resp.status_code == 200
            data = resp.json()
            assert data["status"] == "ready"
            assert data["database"] == "connected"
            mock_init.assert_awaited_once()


@pytest.mark.asyncio
async def test_ready_endpoint_unavailable():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        with patch("src.main._db_initialized", True), patch(
            "src.main.check_db_health", new_callable=AsyncMock
        ) as mock_health:
            mock_health.return_value = False
            resp = await client.get("/ready")
            assert resp.status_code == 503
            assert "Database connectivity unavailable" in resp.json()["detail"]


@pytest.mark.asyncio
async def test_register_endpoint():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.get("/register")
        assert resp.status_code == 200
        kits = resp.json()
        assert len(kits) == 1
        assert kits[0]["bucket"] == "PO_POLICIES"
        assert kits[0]["domain"] == "People Operations"
        tool_names = [t["name"] for t in kits[0]["tools"]]
        assert tool_names == ["query_policy", "query_training"]


@pytest.mark.asyncio
async def test_tools_query_policy_success():
    transport = ASGITransport(app=app)
    token = _make_test_jwt()
    mock_rows = [
        {
            "document_name": "Sabbatical Leave Policy.pdf",
            "excerpt": "Employees with 3 years of continuous tenure are eligible.",
            "similarity_score": 0.87,
        }
    ]
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        with patch.object(settings, "POLICY_DRIVE_FOLDER_ID", "policy-folder-123"), patch(
            "src.main.search_similar_chunks", new_callable=AsyncMock, return_value=mock_rows
        ) as mock_search:
            resp = await client.post(
                "/tools/query_policy",
                json={"query": "What is the sabbatical leave eligibility?", "top_k": 3},
                headers={"X-JWT-Assertion": token},
            )
            assert resp.status_code == 200
            data = resp.json()
            assert data["query"] == "What is the sabbatical leave eligibility?"
            assert data["source_count"] == 1
            assert data["results"][0]["document"] == "Sabbatical Leave Policy.pdf"
            assert data["results"][0]["similarity"] == pytest.approx(0.87)
            mock_search.assert_awaited_once()
            assert mock_search.call_args.kwargs["drive_folder_id"] == "policy-folder-123"
            assert mock_search.call_args.kwargs["top_k"] == 3


@pytest.mark.asyncio
async def test_tools_query_training_success():
    transport = ASGITransport(app=app)
    token = _make_test_jwt()
    mock_rows = [
        {
            "document_name": "Certification Playbook.pdf",
            "excerpt": "Use promo code WSO2CERT100 for free exam registration.",
            "similarity_score": 0.91,
        }
    ]
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        with patch.object(settings, "TRAINING_DRIVE_FOLDER_ID", "training-folder-456"), patch(
            "src.main.search_similar_chunks", new_callable=AsyncMock, return_value=mock_rows
        ) as mock_search:
            resp = await client.post(
                "/tools/query_training",
                json={"query": "How do I get a certification promo code?", "top_k": 25},
                headers={"Authorization": f"Bearer {token}"},
            )
            assert resp.status_code == 200
            data = resp.json()
            assert data["source_count"] == 1
            # top_k is capped at 10
            assert mock_search.call_args.kwargs["top_k"] == 10
            assert mock_search.call_args.kwargs["drive_folder_id"] == "training-folder-456"


@pytest.mark.asyncio
async def test_tools_validation_and_auth_errors():
    transport = ASGITransport(app=app)
    token = _make_test_jwt()
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        # 1. Unknown tool -> 404
        resp = await client.post(
            "/tools/unknown_tool",
            json={"query": "test"},
            headers={"X-JWT-Assertion": token},
        )
        assert resp.status_code == 404

        # 2. Invalid tool name characters -> 400
        resp = await client.post(
            "/tools/bad$tool!",
            json={"query": "test"},
            headers={"X-JWT-Assertion": token},
        )
        assert resp.status_code == 400

        # 3. Missing auth token when DEV_STAGING_TOKEN is unset -> 401
        with patch.object(settings, "DEV_STAGING_TOKEN", None), patch.object(
            settings, "DEV_MOCK_USER_ENABLED", False
        ), patch.dict("os.environ", {"DEV_STAGING_TOKEN": "", "DEV_MOCK_USER_ENABLED": "false"}):
            resp = await client.post("/tools/query_policy", json={"query": "test"})
            assert resp.status_code == 401

        # 4. Empty query -> 400
        resp = await client.post(
            "/tools/query_policy",
            json={"query": "   "},
            headers={"X-JWT-Assertion": token},
        )
        assert resp.status_code == 400

        # 5. Oversized query (>4096 chars) -> 400
        resp = await client.post(
            "/tools/query_policy",
            json={"query": "x" * 4097},
            headers={"X-JWT-Assertion": token},
        )
        assert resp.status_code == 400


@pytest.mark.asyncio
async def test_concurrent_ensure_db_initialized():
    from src.main import ensure_db_initialized

    init_calls = 0

    async def suspending_init_db():
        nonlocal init_calls
        init_calls += 1
        await asyncio.sleep(0.04)
        return None

    with patch("src.main._db_initialized", False), patch(
        "src.main.init_db", side_effect=suspending_init_db
    ):
        results = await asyncio.gather(*[ensure_db_initialized() for _ in range(5)])
        assert all(results)
        assert init_calls == 1


@pytest.mark.asyncio
async def test_lifespan_shutdown_cancels_and_awaits_background_workers():
    from src.main import _background_tasks, _track_background_task, app, lifespan

    worker_cancelled = False

    async def long_running_worker():
        nonlocal worker_cancelled
        try:
            await asyncio.sleep(100)
        except asyncio.CancelledError:
            worker_cancelled = True
            raise

    with patch(
        "src.main.ensure_db_initialized", new_callable=AsyncMock, return_value=True
    ), patch("src.main.close_db_pool", new_callable=AsyncMock) as mock_close, patch(
        "src.config.settings.SYNC_ENABLED", False
    ):
        async with lifespan(app):
            task = _track_background_task(long_running_worker())
            await asyncio.sleep(0.01)
            assert task in _background_tasks
            assert not task.done()

        assert worker_cancelled
        assert task.done()
        assert task.cancelled()
        assert task not in _background_tasks
        mock_close.assert_called_once()
