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
from typing import Any, Dict, List, Optional
from apscheduler.schedulers.asyncio import AsyncIOScheduler
from fastapi import BackgroundTasks, Depends, FastAPI, Header, HTTPException, Response, status
from pydantic import BaseModel, Field

from .config import settings
from .db.client import (
    check_db_health,
    close_db_pool,
    get_status_summary,
    init_db,
    search_similar_chunks,
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


async def verify_admin_auth(x_admin_key: Optional[str] = Header(None, alias="X-Admin-Key")) -> None:
    """Validates X-Admin-Key header when ADMIN_API_KEY is configured."""
    if not settings.ADMIN_API_KEY:
        return
    if not x_admin_key or x_admin_key != settings.ADMIN_API_KEY:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Invalid or missing X-Admin-Key header",
        )


async def _startup_sync_worker() -> None:
    """Runs initial sync with retry backoff for Kubernetes DNS/network readiness."""
    retry_delays = [2, 15, 60]
    for attempt, delay in enumerate(retry_delays, start=1):
        await asyncio.sleep(delay)
        logger.info(f"Executing startup sync (attempt {attempt}/{len(retry_delays)})...")
        try:
            results = await run_full_sync()
            if results.get("status") == "completed":
                logger.info("Startup synchronization succeeded.")
                return
        except Exception as e:
            logger.warning(f"Startup sync attempt {attempt} failed: {e}")

    logger.warning("Startup sync attempts completed. Scheduled interval will run normally.")


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Lifecycle manager for database initialization and background scheduler."""
    logger.info(f"Starting Policy Ingestion Service on port {settings.PORT}...")

    # Initialize PostgreSQL and schema
    try:
        await init_db()
    except Exception as e:
        logger.error(f"Initial database setup encountered an error: {e}")

    # Start recurring sync scheduler if enabled
    if settings.SYNC_ENABLED:
        interval_hours = max(1, settings.POLICY_SYNC_INTERVAL_HOURS)
        scheduler.add_job(
            run_full_sync,
            "interval",
            hours=interval_hours,
            id="policy_full_sync",
            name="Periodic Google Drive Policy Sync",
            replace_existing=True,
        )
        scheduler.start()
        logger.info(f"Scheduled periodic Drive sync every {interval_hours} hour(s).")

        if settings.SYNC_ON_STARTUP:
            asyncio.create_task(_startup_sync_worker())

    yield

    # Clean shutdown
    if scheduler.running:
        scheduler.shutdown(wait=False)
    await close_db_pool()
    logger.info("Policy Ingestion Service stopped.")


app = FastAPI(
    title="WSO2 Policy Ingestion Service",
    description="Extracts documents from Google Drive, chunks, embeds, and stores in pgvector.",
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


class FolderSummary(BaseModel):
    drive_folder_id: str = Field(..., description="Google Drive folder identifier")
    doc_count: int = Field(..., description="Count of documents indexed from folder")


class AdminStatusResponse(BaseModel):
    total_documents: int = Field(..., description="Total documents in pgvector")
    total_chunks: int = Field(..., description="Total chunks in pgvector")
    folders: List[FolderSummary] = Field(default_factory=list, description="Stats per folder")
    last_synced_at: Optional[str] = Field(None, description="ISO timestamp of last successful sync")
    sync_scheduler_running: bool = Field(..., description="Status of APScheduler")
    sync_interval_hours: int = Field(..., description="Scheduled interval in hours")


class SyncResponse(BaseModel):
    status: str = Field(..., description="Sync execution status (accepted or completed)")
    message: Optional[str] = Field(None, description="Status detail message")
    folders: Optional[List[Dict[str, Any]]] = Field(None, description="Detailed folder sync stats")
    total_elapsed_seconds: Optional[float] = Field(None, description="Total execution duration")


class EmbedRequest(BaseModel):
    text: Optional[str] = Field(None, description="Single text string to embed")
    texts: Optional[List[str]] = Field(None, description="Batch of texts to embed")


class EmbedResponse(BaseModel):
    embedding: Optional[List[float]] = Field(None, description="768-dim vector for single text")
    dimension: Optional[int] = Field(None, description="Vector dimension")
    embeddings: Optional[List[List[float]]] = Field(None, description="Vectors for batch input")
    count: Optional[int] = Field(None, description="Number of embeddings generated")


class SearchRequest(BaseModel):
    query: str = Field(..., description="Natural language question or search text")
    drive_folder_id: Optional[str] = Field(None, description="Filter by specific folder ID")
    top_k: int = Field(5, ge=1, le=20, description="Max number of excerpts to return")


class SearchResultItem(BaseModel):
    document_name: str = Field(..., description="Source document file name")
    excerpt: str = Field(..., description="Text chunk content")
    chunk_index: int = Field(..., description="Sequential chunk index")
    drive_folder_id: str = Field(..., description="Google Drive folder identifier")
    similarity_score: float = Field(..., description="Cosine similarity score (0.0 to 1.0)")


class SearchResponse(BaseModel):
    query: str = Field(..., description="Original search query")
    results_count: int = Field(..., description="Number of results returned")
    results: List[SearchResultItem] = Field(default_factory=list, description="Ranked results")


# --- Endpoints ---


@app.get("/health", response_model=HealthResponse, tags=["Monitoring"])
async def health() -> HealthResponse:
    """Liveness probe."""
    return HealthResponse(status="ok", service="policy-ingestion")


@app.get("/ready", response_model=ReadyResponse, tags=["Monitoring"])
async def ready() -> ReadyResponse:
    """Readiness probe verifying database connectivity."""
    is_healthy = await check_db_health()
    if not is_healthy:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail="Database connectivity unavailable",
        )
    return ReadyResponse(status="ready", database="connected")


@app.get(
    "/admin/status",
    response_model=AdminStatusResponse,
    dependencies=[Depends(verify_admin_auth)],
    tags=["Admin"]
)
async def admin_status() -> AdminStatusResponse:
    """Returns database summary: document and chunk counts per folder."""
    try:
        summary = await get_status_summary()
        summary["sync_scheduler_running"] = scheduler.running
        summary["sync_interval_hours"] = settings.POLICY_SYNC_INTERVAL_HOURS
        return AdminStatusResponse(**summary)
    except Exception as e:
        logger.error(f"Failed to fetch admin status: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@app.post(
    "/admin/sync",
    response_model=SyncResponse,
    status_code=status.HTTP_202_ACCEPTED,
    dependencies=[Depends(verify_admin_auth)],
    tags=["Admin"]
)
async def trigger_sync(
    background_tasks: BackgroundTasks,
    response: Response,
    wait: bool = False
) -> SyncResponse:
    """
    Manually triggers full Google Drive synchronization.
    If wait=True, blocks until sync completes and returns stats with HTTP 200.
    """
    if wait:
        results = await run_full_sync()
        response.status_code = status.HTTP_200_OK
        return SyncResponse(
            status=results.get("status", "completed"),
            folders=results.get("folders"),
            total_elapsed_seconds=results.get("total_elapsed_seconds"),
        )

    background_tasks.add_task(run_full_sync)
    return SyncResponse(
        status="accepted",
        message="Drive synchronization scheduled in background",
    )


@app.post("/embed", response_model=EmbedResponse, tags=["Utility"])
async def embed_endpoint(payload: EmbedRequest) -> EmbedResponse:
    """
    Utility endpoint providing query and text embeddings.
    Can be leveraged directly by the Policy MCP or test runners.
    """
    if payload.text:
        vector = await embedder.embed_query(payload.text)
        return EmbedResponse(embedding=vector, dimension=len(vector))
    elif payload.texts:
        vectors = await embedder.embed_texts(payload.texts)
        return EmbedResponse(embeddings=vectors, count=len(vectors))
    else:
        raise HTTPException(status_code=400, detail="Either 'text' or 'texts' is required")


@app.post("/search", response_model=SearchResponse, tags=["Search"])
async def search_endpoint(payload: SearchRequest) -> SearchResponse:
    """
    Direct semantic vector search against policy chunks.
    Validates end-to-end embedding and pgvector cosine ranking.
    """
    try:
        query_vector = await embedder.embed_query(payload.query)
        results = await search_similar_chunks(
            embedding=query_vector,
            drive_folder_id=payload.drive_folder_id,
            top_k=payload.top_k,
        )
        search_items = [
            SearchResultItem(
                document_name=r["document_name"],
                excerpt=r["excerpt"],
                chunk_index=r["chunk_index"],
                drive_folder_id=r["drive_folder_id"],
                similarity_score=float(r["similarity_score"]),
            )
            for r in results
        ]
        return SearchResponse(
            query=payload.query,
            results_count=len(search_items),
            results=search_items,
        )
    except Exception as e:
        logger.error(f"Search endpoint error: {e}")
        raise HTTPException(status_code=500, detail=str(e))
