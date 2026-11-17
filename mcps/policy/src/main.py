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

"""Unified FastAPI Policy MCP Server & Google Drive Ingestion Pipeline."""

import asyncio
import json
import logging
import os
import re
from contextlib import asynccontextmanager
from typing import List, Optional, Set

from apscheduler.schedulers.asyncio import AsyncIOScheduler
from fastapi import FastAPI, HTTPException, Request, status
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field

from .auth.jwt import (
    extract_email_from_jwt,
    is_permitted_dev_environment,
    resolve_token,
)
from .config import settings
from .db.client import (
    check_db_health,
    close_db_pool,
    init_db,
    search_similar_chunks,
)
from .ingestion.embedder import embedder
from .ingestion.pipeline import run_full_sync
from .mcp.buckets import get_policy_bucket, get_tool
from .mcp.models import BucketKit, SearchResultItem, ToolResponse

# Structured logging setup
logging.basicConfig(
    level=getattr(logging, settings.LOG_LEVEL.upper(), logging.INFO),
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
logger = logging.getLogger("policy_mcp")

VALID_TOOL_NAME_REGEX = re.compile(r"^[a-zA-Z0-9_-]+$")
MAX_REQUEST_BODY_BYTES = 1024 * 1024
MAX_QUERY_LENGTH = 4096

scheduler = AsyncIOScheduler()
_db_init_lock: Optional[asyncio.Lock] = None
_db_init_loop: Optional[asyncio.AbstractEventLoop] = None
_db_initialized: bool = False
_background_tasks: Set[asyncio.Task] = set()


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
    """Lifecycle manager for database initialization and background Drive sync scheduler."""
    logger.info(f"Starting Unified Policy MCP Server on port {settings.PORT}...")

    if not await ensure_db_initialized():
        logger.warning("Initial database setup failed, starting background retry worker...")
        _track_background_task(_init_db_with_retry())

    # Pre-warm local ONNX model in background so first MCP query has zero cold-start delay
    _track_background_task(embedder.warmup())

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

    if scheduler.running:
        scheduler.shutdown(wait=False)
    for task in list(_background_tasks):
        task.cancel()
    if _background_tasks:
        await asyncio.gather(*_background_tasks, return_exceptions=True)
    await close_db_pool()
    logger.info("Policy MCP Server stopped.")


app = FastAPI(
    title="WSO2 Policy MCP Server",
    description=(
        "Unified Model Context Protocol (MCP) server and automated Google Drive ingestion "
        "pipeline for semantic vector search over HR and Training documents."
    ),
    version="1.0.0",
    lifespan=lifespan,
)


class HealthResponse(BaseModel):
    """Health check response payload."""

    status: str = Field("ok", description="Service health status")
    service: str = Field("policy-mcp", description="Service identifier")
    database: str = Field("connected", description="Database connectivity status")


class ReadyResponse(BaseModel):
    """Readiness check response payload."""

    status: str = Field("ready", description="Service readiness status")
    database: str = Field("connected", description="Database connection status")


@app.get("/health", response_model=HealthResponse, tags=["Monitoring"])
async def health() -> JSONResponse:
    """Verifies service liveness and PostgreSQL database connectivity."""
    try:
        is_healthy = await asyncio.wait_for(check_db_health(), timeout=5.0)
    except Exception:
        is_healthy = False

    if not is_healthy:
        return JSONResponse(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            content={
                "status": "degraded",
                "service": "policy-mcp",
                "database": "unreachable",
            },
        )
    return JSONResponse(
        status_code=status.HTTP_200_OK,
        content={
            "status": "ok",
            "service": "policy-mcp",
            "database": "connected",
        },
    )


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


@app.get("/register", response_model=List[BucketKit], tags=["MCP"])
async def register() -> List[BucketKit]:
    """Handles capability discovery requests from Agent Core."""
    return [get_policy_bucket()]


@app.post("/tools/{tool_name}", response_model=ToolResponse, tags=["MCP"])
async def execute_tool(tool_name: str, request: Request) -> JSONResponse:
    """Executes semantic policy and training queries in-process."""
    if not VALID_TOOL_NAME_REGEX.match(tool_name):
        return JSONResponse(
            status_code=status.HTTP_400_BAD_REQUEST,
            content={"error": "Invalid tool name format"},
        )

    if get_tool(tool_name) is None:
        return JSONResponse(
            status_code=status.HTTP_404_NOT_FOUND,
            content={"error": f"Unknown tool '{tool_name}'"},
        )

    # 1. Extract and validate user JWT identity for audit trail
    jwt_token = request.headers.get("X-JWT-Assertion") or ""
    if not jwt_token:
        auth_header = request.headers.get("Authorization") or ""
        if auth_header.startswith("Bearer "):
            jwt_token = auth_header[len("Bearer "):]
    resolved_token = resolve_token(jwt_token)
    user_email = await asyncio.to_thread(extract_email_from_jwt, resolved_token)

    mock_allowed = is_permitted_dev_environment() and (
        settings.DEV_MOCK_USER_ENABLED
        or os.getenv("DEV_MOCK_USER_ENABLED", "").lower() == "true"
    )
    if not user_email and not mock_allowed:
        return JSONResponse(
            status_code=status.HTTP_401_UNAUTHORIZED,
            content={"error": "Unauthorized: valid user token required"},
        )

    # 2. Enforce 1 MB payload size limit and parse JSON arguments
    raw_body = await request.body()
    if len(raw_body) > MAX_REQUEST_BODY_BYTES:
        return JSONResponse(
            status_code=status.HTTP_400_BAD_REQUEST,
            content={"error": "Invalid request JSON or payload too large"},
        )
    try:
        args = json.loads(raw_body.decode("utf-8"))
        if not isinstance(args, dict):
            raise ValueError("Payload must be a JSON object")
    except Exception:
        return JSONResponse(
            status_code=status.HTTP_400_BAD_REQUEST,
            content={"error": "Invalid request JSON or payload too large"},
        )

    query = args.get("query")
    if not isinstance(query, str) or not query.strip():
        return JSONResponse(
            status_code=status.HTTP_400_BAD_REQUEST,
            content={"error": "Missing required argument 'query'"},
        )

    if len(query) > MAX_QUERY_LENGTH:
        return JSONResponse(
            status_code=status.HTTP_400_BAD_REQUEST,
            content={
                "error": f"Query exceeds maximum allowed length of {MAX_QUERY_LENGTH} characters"
            },
        )

    top_k = 5
    raw_top_k = args.get("top_k")
    if isinstance(raw_top_k, (int, float)) and not isinstance(raw_top_k, bool) and raw_top_k > 0:
        top_k = min(int(raw_top_k), 10)

    # 3. Resolve target Google Drive folder ID for the requested tool
    folder_id = ""
    if tool_name == "query_policy":
        folder_id = settings.policy_folder_id
    elif tool_name == "query_training":
        folder_id = settings.training_folder_id

    if not folder_id:
        logger.error("Target folder ID not configured for tool '%s'", tool_name)
        return JSONResponse(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            content={"error": "Target folder ID not configured for this tool"},
        )

    logger.info(
        "Executing policy search tool=%s user=%s queryLength=%d",
        tool_name,
        user_email or "dev-mock-user",
        len(query),
    )

    # 4. In-process query embedding with BAAI/bge-base-en-v1.5 + LRU cache
    try:
        query_vector = await asyncio.wait_for(
            embedder.embed_query(query),
            timeout=settings.SEARCH_TIMEOUT_SECONDS,
        )
    except Exception as e:
        logger.error("Failed to embed query: %s", e)
        return JSONResponse(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            content={"error": "Failed to embed query"},
        )

    # 5. Execute pgvector cosine similarity search
    try:
        rows = await asyncio.wait_for(
            search_similar_chunks(query_vector, drive_folder_id=folder_id, top_k=top_k),
            timeout=settings.SEARCH_TIMEOUT_SECONDS,
        )
    except Exception as e:
        logger.error("Database vector search error: %s", e)
        return JSONResponse(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            content={"error": "Vector search execution failed"},
        )

    results = [
        SearchResultItem(
            document=row["document_name"],
            excerpt=row["excerpt"],
            similarity=float(row["similarity_score"]),
        )
        for row in rows
    ]

    resp_payload = ToolResponse(
        query=query,
        results=results,
        source_count=len(results),
    )
    return JSONResponse(
        status_code=status.HTTP_200_OK,
        content=resp_payload.model_dump(),
    )
