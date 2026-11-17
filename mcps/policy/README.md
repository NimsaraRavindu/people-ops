# WSO2 Policy MCP Server (Unified Python Service)

Unified **Model Context Protocol (MCP)** server and automated **Google Drive Ingestion Pipeline** for WSO2 People Operations HR Policies and Training Playbooks.

## Architecture Highlights
- **Single Unified Service (`PORT=8003`)**: Combines MCP capability discovery (`/register`), semantic vector search (`/tools/query_policy`, `/tools/query_training`), and scheduled Google Drive document ingestion (`APScheduler`) in one FastAPI process.
- **In-Process MIT-Licensed Embeddings**: Uses `fastembed` with **`BAAI/bge-base-en-v1.5`** (768 dimensions, INT8 ONNX CPU) plus a thread-safe **LRU TTL query cache** (`QueryEmbeddingCache`) with zero external API keys or network hops.
- **PostgreSQL `pgvector` Backend**: Stores document metadata (`policy_documents`) and 768-dim HNSW-indexed chunks (`policy_chunks`), supporting `hnsw.iterative_scan = 'relaxed_order'` and automatic fallback for folder-filtered queries.

## Endpoints
| Method | Path | Description |
| :--- | :--- | :--- |
| `GET` | `/health` | Service liveness & PostgreSQL connectivity check |
| `GET` | `/ready` | Readiness check verifying schema initialization & DB health |
| `GET` | `/register` | Returns `PO_POLICIES` capability bucket (`query_policy`, `query_training`) |
| `POST` | `/tools/query_policy` | Semantic search over HR policy documents |
| `POST` | `/tools/query_training` | Semantic search over Training & Certification playbooks |

## Local Development

```bash
cd mcps/policy
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt

# Start server on port 8003
uvicorn src.main:app --host 0.0.0.0 --port 8003 --reload
```

## Running Tests

```bash
cd mcps/policy
.venv/bin/pytest tests/ -k "not policy_rag_model_comparison" -v
```
