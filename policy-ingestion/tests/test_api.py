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

import asyncio
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
        with patch("src.main._db_initialized", True), \
             patch("src.main.check_db_health", new_callable=AsyncMock) as mock_health:
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
        with patch("src.main._db_initialized", False), \
             patch("src.main.init_db", side_effect=RuntimeError("connection refused")):
            resp = await client.get("/ready")
            assert resp.status_code == 503
            assert "Database schema initialization failed" in resp.json()["detail"]


@pytest.mark.asyncio
async def test_ready_endpoint_recovers_after_db_initialized():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        with patch("src.main._db_initialized", False), \
             patch("src.main.init_db", new_callable=AsyncMock) as mock_init, \
             patch("src.main.check_db_health", new_callable=AsyncMock) as mock_health:
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
        with patch("src.main._db_initialized", True), \
             patch("src.main.check_db_health", new_callable=AsyncMock) as mock_health:
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
async def test_embed_endpoint_oversized_batch_item():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        payload = {"texts": ["x" * 10001]}
        resp = await client.post("/embed", json=payload)
        assert resp.status_code == 400
        assert "exceeds maximum length" in resp.json()["detail"]


@pytest.mark.asyncio
async def test_concurrent_ensure_db_initialized():
    import asyncio
    from src.main import ensure_db_initialized

    init_calls = 0

    async def suspending_init_db():
        nonlocal init_calls
        init_calls += 1
        # Explicitly suspend to guarantee concurrent callers overlap during initialization
        await asyncio.sleep(0.04)
        return None

    with patch("src.main._db_initialized", False), \
         patch("src.main.init_db", side_effect=suspending_init_db):
        results = await asyncio.gather(*[ensure_db_initialized() for _ in range(5)])
        assert all(results)
        assert init_calls == 1


@pytest.mark.asyncio
async def test_concurrent_failure_followed_by_recovery():
    import asyncio
    from src.main import ensure_db_initialized

    call_count = 0

    async def failing_then_suspending_init():
        nonlocal call_count
        call_count += 1
        if call_count == 1:
            await asyncio.sleep(0.02)
            raise RuntimeError("Transient database network failure")
        await asyncio.sleep(0.03)
        return None

    with patch("src.main._db_initialized", False), \
         patch("src.main.init_db", side_effect=failing_then_suspending_init):
        # 3 tasks launch simultaneously
        results = await asyncio.gather(
            ensure_db_initialized(),
            ensure_db_initialized(),
            ensure_db_initialized(),
        )
        # Task 1 failed, Task 2 recovered and succeeded, Task 3 observed initialized state
        assert results == [False, True, True]
        assert call_count == 2


@pytest.mark.asyncio
async def test_scheduled_sync_wrapper_skips_when_uninitialized():
    from src.main import _scheduled_sync_wrapper
    with patch("src.main.ensure_db_initialized", new_callable=AsyncMock) as mock_ensure, \
         patch("src.main.run_full_sync", new_callable=AsyncMock) as mock_sync:
        mock_ensure.return_value = False
        await _scheduled_sync_wrapper()
        mock_sync.assert_not_called()


@pytest.mark.asyncio
async def test_scheduled_sync_wrapper_runs_when_initialized():
    from src.main import _scheduled_sync_wrapper
    with patch("src.main.ensure_db_initialized", new_callable=AsyncMock) as mock_ensure, \
         patch("src.main.run_full_sync", new_callable=AsyncMock) as mock_sync:
        mock_ensure.return_value = True
        mock_sync.return_value = {"status": "completed"}
        await _scheduled_sync_wrapper()
        mock_sync.assert_called_once()


@pytest.mark.asyncio
async def test_startup_recovery_worker():
    from src.main import _init_db_with_retry
    with patch("src.main._db_initialized", False), \
         patch("src.main.init_db", new_callable=AsyncMock) as mock_init, \
         patch("asyncio.sleep", new_callable=AsyncMock):
        mock_init.side_effect = [RuntimeError("DB temporarily down"), None]
        await _init_db_with_retry()
        assert mock_init.await_count == 2


@pytest.mark.asyncio
async def test_db_init_lock_retained_when_loop_attribute_is_none():
    from src.main import _get_db_init_lock

    lock1 = _get_db_init_lock()
    # Simulate Python 3.11 uncontended lock behavior where _loop is cleared
    if hasattr(lock1, "_loop"):
        lock1._loop = None

    lock2 = _get_db_init_lock()
    assert lock1 is lock2, "Init lock must not be recreated when running on the same event loop"


@pytest.mark.asyncio
async def test_pool_lock_retained_when_loop_attribute_is_none():
    from src.db.client import _get_pool_lock

    lock1 = _get_pool_lock()
    if hasattr(lock1, "_loop"):
        lock1._loop = None

    lock2 = _get_pool_lock()
    assert lock1 is lock2, "Pool lock must not be recreated when running on the same event loop"


@pytest.mark.asyncio
async def test_lifespan_shutdown_cancels_and_awaits_background_workers():
    from src.main import lifespan, app, _background_tasks, _track_background_task

    worker_cancelled = False

    async def long_running_worker():
        nonlocal worker_cancelled
        try:
            await asyncio.sleep(100)
        except asyncio.CancelledError:
            worker_cancelled = True
            raise

    with patch("src.main.ensure_db_initialized", new_callable=AsyncMock, return_value=True), \
         patch("src.main.close_db_pool", new_callable=AsyncMock) as mock_close, \
         patch("src.config.settings.SYNC_ENABLED", False):
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


@pytest.mark.asyncio
async def test_lifespan_shutdown_active_sync_finishes_before_pool_closed():
    from src.main import lifespan, app, _background_tasks, _track_background_task

    pool_closed = False
    active_sync_in_progress = False
    sync_cleanup_completed = False
    cleanup_happened_while_pool_open = False

    async def fake_close_db_pool():
        nonlocal pool_closed
        pool_closed = True

    async def active_sync_worker():
        nonlocal active_sync_in_progress, sync_cleanup_completed, cleanup_happened_while_pool_open
        active_sync_in_progress = True
        try:
            # Simulate active document extraction & chunk embedding loop
            while True:
                await asyncio.sleep(0.01)
        except asyncio.CancelledError:
            # Simulate in-flight database transaction rollback / buffer flush
            await asyncio.sleep(0.03)
            sync_cleanup_completed = True
            if not pool_closed:
                cleanup_happened_while_pool_open = True
            raise

    with patch("src.main.ensure_db_initialized", new_callable=AsyncMock, return_value=True), \
         patch("src.main.close_db_pool", side_effect=fake_close_db_pool), \
         patch("src.config.settings.SYNC_ENABLED", False):
        async with lifespan(app):
            task = _track_background_task(active_sync_worker())
            await asyncio.sleep(0.02)
            assert active_sync_in_progress
            assert task in _background_tasks
            assert not task.done()
            assert not pool_closed

        # Verify task was awaited and cleanup finished BEFORE pool was closed
        assert sync_cleanup_completed, "Active sync cleanup must complete on cancellation"
        assert cleanup_happened_while_pool_open, (
            "Worker cleanup must finish before close_db_pool is called"
        )
        assert pool_closed, "Database pool must be closed after workers finish"
        assert task.done()
        assert task.cancelled()
        assert task not in _background_tasks


@pytest.mark.asyncio
async def test_embed_internal_auth_protection():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        with patch("src.main.settings.POLICY_INGESTION_SECRET", "super-secret-key"):
            # 1. No token -> 401
            res = await client.post("/embed", json={"text": "hello"})
            assert res.status_code == 401

            # 2. Wrong token -> 401
            res = await client.post("/embed", json={"text": "hello"}, headers={"X-Internal-Token": "wrong"})
            assert res.status_code == 401

            # 3. Valid X-Internal-Token -> 200
            with patch("src.main.embedder.embed_query", new_callable=AsyncMock, return_value=[0.1] * 768):
                res = await client.post(
                    "/embed",
                    json={"text": "hello"},
                    headers={"X-Internal-Token": "super-secret-key"},
                )
                assert res.status_code == 200

            # 4. Valid Bearer Token -> 200
            with patch("src.main.embedder.embed_query", new_callable=AsyncMock, return_value=[0.1] * 768):
                res = await client.post(
                    "/embed",
                    json={"text": "hello"},
                    headers={"Authorization": "Bearer super-secret-key"},
                )
                assert res.status_code == 200


@pytest.mark.asyncio
async def test_embed_batch_limit_rejection():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        # Batch exceeding 50 items rejected
        too_many = ["chunk"] * 51
        res = await client.post("/embed", json={"texts": too_many})
        assert res.status_code == 422
