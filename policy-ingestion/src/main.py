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

# FastAPI entry point for Policy Ingestion Service with APScheduler.

import asyncio
import logging
from contextlib import asynccontextmanager
from typing import List, Optional, Set
from apscheduler.schedulers.asyncio import AsyncIOScheduler
from fastapi import FastAPI, HTTPException, Request, status
from pydantic import BaseModel, Field

from .config import settings
from .db.client import (
    check_db_health,
    close_db_pool,
    init_db,
)
from .ingestion.embedder import embedder
from .ingestion.pipeline import run_full_sync

# Structured logging setup
logging.basicConfig(
    level=getattr(logging, settings.LOG_LEVEL.upper(), logging.INFO),
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
logger = logging.getLogger("policy_ingestion")

scheduler = AsyncIOScheduler()
_db_init_lock: Optional[asyncio.Lock] = None
_db_init_loop: Optional[asyncio.AbstractEventLoop] = None
_db_initialized: bool = False
_background_tasks: Set[asyncio.Task] = set()
_embed_semaphore: Optional[asyncio.Semaphore] = None


def _get_embed_semaphore() -> asyncio.Semaphore:
    """Returns the embedding concurrency semaphore bound to the event loop."""
    global _embed_semaphore
    if _embed_semaphore is None:
        _embed_semaphore = asyncio.Semaphore(10)
    return _embed_semaphore


def _verify_internal_auth(request: Request) -> None:
    """Validates shared secret token if POLICY_INGESTION_SECRET is configured."""
    if not settings.POLICY_INGESTION_SECRET:
        return
    token = request.headers.get("X-Internal-Token") or ""
    if not token and "Authorization" in request.headers:
        auth_h = request.headers["Authorization"]
        if auth_h.startswith("Bearer "):
            token = auth_h[7:].strip()
    if token != settings.POLICY_INGESTION_SECRET:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Unauthorized: invalid internal service token",
        )


def _track_background_task(coro) -> asyncio.Task:
    """Tracks background worker tasks to guarantee cancellation and await during shutdown."""
    task = asyncio.create_task(coro)
    _background_tasks.add(task)
    task.add_done_callback(_background_tasks.discard)
    return task


def _get_db_init_lock() -> asyncio.Lock:
    """Returns the initialization lock bound to the running event loop."""
    global _db_init_lock, _db_init_loop
    try:
        loop = asyncio.get_running_loop()
        if _db_init_lock is None or _db_init_loop is not loop:
            _db_init_lock = asyncio.Lock()
            _db_init_loop = loop
    except RuntimeError:
        if _db_init_lock is None:
            _db_init_lock = asyncio.Lock()
            _db_init_loop = None
    return _db_init_lock


async def ensure_db_initialized() -> bool:
    """Serializes database initialization across concurrent callers."""
    global _db_initialized
    if _db_initialized:
        return True
    async with _get_db_init_lock():
        if _db_initialized:
            return True
        try:
            await init_db()
            _db_initialized = True
            logger.info("Database schema initialized successfully.")
            return True
        except Exception as e:
            logger.warning(f"Database schema initialization attempt failed: {e}")
            return False


async def _init_db_with_retry() -> None:
    """Retries database initialization with exponential backoff on startup network failures."""
    if _db_initialized:
        return
    retry_delays = [2, 5, 10, 30]
    for attempt, delay in enumerate(retry_delays, start=1):
        await asyncio.sleep(delay)
        if await ensure_db_initialized():
            return
        logger.warning(
            f"Database retry worker attempt {attempt}/{len(retry_delays)} failed. Retrying..."
        )

    if not await ensure_db_initialized():
        logger.error("Final database setup attempt in retry worker failed.")


async def _startup_sync_worker() -> None:
    """Runs initial sync with retry backoff for Kubernetes DNS/network readiness."""
    retry_delays = [2, 15, 60]
    for attempt, delay in enumerate(retry_delays, start=1):
        await asyncio.sleep(delay)
        if not await ensure_db_initialized():
            logger.warning(f"Startup sync attempt {attempt} skipped: database uninitialized")
            continue

        logger.info(f"Executing startup sync (attempt {attempt}/{len(retry_delays)})...")
        try:
            results = await run_full_sync()
            if results.get("status") == "completed":
                logger.info("Startup synchronization succeeded.")
                return
        except Exception as e:
            logger.warning(f"Startup sync attempt {attempt} failed: {e}")

    logger.warning("Startup sync attempts completed. Scheduled interval will run normally.")


async def _scheduled_sync_wrapper() -> None:
    """Wrapper for recurring scheduler job, gating execution on database readiness."""
    if not await ensure_db_initialized():
        logger.warning("Scheduled Drive sync skipped: database schema is uninitialized.")
        return
    await run_full_sync()


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Lifecycle manager for database initialization and background scheduler."""
    logger.info(f"Starting Policy Ingestion Service on port {settings.PORT}...")

    # Initialize PostgreSQL and schema with retry fallback
    if not await ensure_db_initialized():
        logger.warning("Initial database setup failed, starting background retry worker...")
        _track_background_task(_init_db_with_retry())

    # Start recurring sync scheduler if enabled
    if settings.SYNC_ENABLED:
        interval_hours = max(1, settings.POLICY_SYNC_INTERVAL_HOURS)
        scheduler.add_job(
            _scheduled_sync_wrapper,
            "interval",
            hours=interval_hours,
            id="policy_full_sync",
            name="Periodic Google Drive Policy Sync",
            replace_existing=True,
        )
        scheduler.start()
        logger.info(f"Scheduled periodic Drive sync every {interval_hours} hour(s).")

        if settings.SYNC_ON_STARTUP:
            _track_background_task(_startup_sync_worker())

    yield

    # Clean shutdown: cancel and await all background workers before closing pool
    if scheduler.running:
        scheduler.shutdown(wait=False)
    for task in list(_background_tasks):
        task.cancel()
    if _background_tasks:
        await asyncio.gather(*_background_tasks, return_exceptions=True)
    await close_db_pool()
    logger.info("Policy Ingestion Service stopped.")


app = FastAPI(
    title="WSO2 Policy Ingestion Service",
    description="Automated background service for syncing and embedding HR policy documents into pgvector.",
    version="1.0.0",
    lifespan=lifespan,
)


# --- Request / Response Models ---


class HealthResponse(BaseModel):
    status: str = Field("ok", description="Service health status")
    service: str = Field("policy-ingestion", description="Service identifier")


class ReadyResponse(BaseModel):
    status: str = Field("ready", description="Service readiness status")
    database: str = Field("connected", description="Database connection status")


class EmbedRequest(BaseModel):
    text: Optional[str] = Field(None, max_length=10000, description="Single text string to embed (max 10,000 chars)")
    texts: Optional[List[str]] = Field(None, max_length=50, description="Batch of texts to embed (max 50 items)")


class EmbedResponse(BaseModel):
    embedding: Optional[List[float]] = Field(None, description="768-dim vector for single text")
    dimension: Optional[int] = Field(None, description="Vector dimension")
    embeddings: Optional[List[List[float]]] = Field(None, description="Vectors for batch input")
    count: Optional[int] = Field(None, description="Number of embeddings generated")


# --- Endpoints ---


@app.get("/health", response_model=HealthResponse, tags=["Monitoring"])
async def health() -> HealthResponse:
    """Liveness probe."""
    return HealthResponse(status="ok", service="policy-ingestion")


@app.get("/ready", response_model=ReadyResponse, tags=["Monitoring"])
async def ready() -> ReadyResponse:
    """Readiness probe verifying database connectivity and schema readiness."""
    if not _db_initialized:
        if not await ensure_db_initialized():
            raise HTTPException(
                status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
                detail="Database schema initialization failed",
            )
    is_healthy = await check_db_health()
    if not is_healthy:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail="Database connectivity unavailable",
        )
    return ReadyResponse(status="ready", database="connected")


@app.post("/embed", response_model=EmbedResponse, tags=["Utility"])
async def embed_endpoint(payload: EmbedRequest, request: Request) -> EmbedResponse:
    """
    Internal embedding endpoint providing query and text vectorization.
    Consumed by the Policy MCP server to perform vector similarity search.
    """
    _verify_internal_auth(request)
    async with _get_embed_semaphore():
        if payload.text:
            vector = await embedder.embed_query(payload.text)
            return EmbedResponse(embedding=vector, dimension=len(vector))
        elif payload.texts:
            for t in payload.texts:
                if len(t) > 10000:
                    raise HTTPException(
                        status_code=400,
                        detail="Individual text in batch exceeds maximum length of 10,000 characters",
                    )
            vectors = await embedder.embed_texts(payload.texts)
            return EmbedResponse(embeddings=vectors, count=len(vectors))
        else:
            raise HTTPException(status_code=400, detail="Either 'text' or 'texts' is required")
