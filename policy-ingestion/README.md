# Policy Ingestion Service

Autonomous background ingestion service for WSO2 People Operations and Training policy documents. 
Extracts policy documents from Google Drive, performs change detection, chunks content into 512-token segments,
embeds them using FastEmbed (`BAAI/bge-base-en-v1.5`), and persists them into PostgreSQL with **pgvector**.

---

## 1. Architecture Overview

```
Google Drive (OAuth 2.0 refresh token)
   │
   ├── HR Policy Folder (Leaves, Benefits, WFH, Sabbatical, etc.)
   └── Training & Certification Playbook Folder
   │
   ▼
Policy Ingestion Service (:8080)
   ├── Extractor (Google Docs, PDF, DOCX, Text)
   ├── Recursive Text Chunker (512 tokens, 50 overlap)
   ├── Local ONNX Embedder (BAAI/bge-base-en-v1.5)
   └── APScheduler (24h sync loop + startup backoff)
   │
   ▼
PostgreSQL + pgvector (policy_documents & policy_chunks)
   ▲
   │ (Read Path: pgvector cosine similarity)
Policy MCP (:8003) ──► Agent Orchestrator
```

---

## 2. Directory Layout

```
policy-ingestion/
├── .choreo/
│   └── component.yaml        # Internal Project-visible REST endpoint
├── src/
│   ├── main.py               # FastAPI entry point & APScheduler lifecycle
│   ├── config.py             # Pydantic environment configuration
│   ├── db/
│   │   ├── client.py         # asyncpg connection pool & pgvector queries
│   │   └── schema.sql        # Table definitions & HNSW vector index
│   ├── drive/
│   │   ├── client.py         # Google Drive API v3 client with OAuth auto-refresh
│   │   └── extractor.py      # Google Docs, PDF, DOCX, text parser
│   └── ingestion/
│       ├── chunker.py        # Recursive character splitter with title headers
│       ├── embedder.py       # FastEmbed ONNX local embedder & mock fallback
│       └── pipeline.py       # Sync orchestrator with atomic transactions
├── requirements.txt
├── Dockerfile
├── openapi.yaml
└── .env.example
```

---

## 3. Configuration

Copy `.env.example` to `.env`:

```bash
cp .env.example .env
```

Key variables:
- **`DB_HOST`**, **`DB_PORT`**, **`DB_NAME`**, **`DB_USER`**, **`DB_PASSWORD`**, **`DB_SSL`**:
  Aiven pgvector instance connection parameters.
- **`POLICY_GOOGLE_OAUTH_CLIENT_ID`**, **`POLICY_GOOGLE_OAUTH_CLIENT_SECRET`**, **`POLICY_GOOGLE_OAUTH_REFRESH_TOKEN`**:
  Google OAuth 2.0 user credentials with `drive.readonly` scope.
- **`POLICY_DRIVE_FOLDER_ID`**: Google Drive folder ID for HR policies.
- **`TRAINING_DRIVE_FOLDER_ID`**: Google Drive folder ID for Training & Certification.
- **`ADMIN_API_KEY`**: Optional shared secret header (`X-Admin-Key`) to guard `/admin/*` endpoints.
- **`EMBEDDING_PROVIDER`**:
  Local ONNX embeddings (`local` with `BAAI/bge-base-en-v1.5`, or `dummy` for mock unit tests).

---

## 4. Running Locally

### Virtual Environment Setup
```bash
python3.11 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

### Start Service
```bash
uvicorn src.main:app --host 0.0.0.0 --port 8080 --reload
```

---

## 5. API Endpoints

| Method | Path | Description |
|:---|:---|:---|
| `GET` | `/health` | Liveness probe (`{"status": "ok"}`). |
| `GET` | `/ready` | Readiness probe (verifies database connectivity). |
| `GET` | `/admin/status` | Returns document & chunk counts, synced folders, and last sync timestamp. |
| `POST` | `/admin/sync` | Manually triggers full Drive sync (pass `?wait=true` to wait for completion). |
| `POST` | `/embed` | Generates 768-dim embeddings for a single text or batch of texts. |
| `POST` | `/search` | Executes semantic cosine similarity search against stored chunks. |
