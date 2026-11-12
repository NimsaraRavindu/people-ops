# Policy MCP Server

Domain Model Context Protocol (MCP) server for WSO2 People Operations and Training policies.
Provides semantic vector search capabilities over official policy documents and certification playbooks stored
in PostgreSQL with **pgvector**.

Registers with the **Agent Orchestrator** via the standard HTTP REST Tool Protocol:
- `GET /health`: Liveness and PostgreSQL database connectivity check.
- `GET /register`: Advertises the `PO_POLICIES` BucketKit containing `query_policy` and `query_training` tools.
- `POST /tools/{tool_name}`: Dispatches incoming semantic search queries to pgvector.

---

## 1. Registered Tools

### A. `query_policy`
- **Scope**: General HR and People Operations policy documents (leave entitlements, benefits, work-from-home,
  code of conduct, disciplinary procedures, and **sabbatical policy**).
- **Arguments**:
  - `query` (string, required): Natural language policy question.
  - `top_k` (integer, optional, default 5, max 10): Maximum number of excerpts to return.
- **Filtering**: Filters `policy_documents` by `POLICY_DRIVE_FOLDER_ID`.

### B. `query_training`
- **Scope**: WSO2 Training & Certification playbook (certification exam tracks, promo codes, badges,
  instructor-led training, and escalation contacts).
- **Arguments**:
  - `query` (string, required): Natural language certification/training question.
  - `top_k` (integer, optional, default 5, max 10): Maximum number of excerpts to return.
- **Filtering**: Filters `policy_documents` by `TRAINING_DRIVE_FOLDER_ID`.

---

## 2. Directory Layout

```
mcps/policy/
├── .choreo/
│   └── component.yaml        # Project-internal REST endpoint (port 8003)
├── .dockerignore              # Prevents secret and artifact leakage into Docker build
├── .env.example              # Sanitized configuration template
├── .gitignore                # Excludes binaries and local environments
├── Dockerfile                # Multi-stage production container (non-root UID 10001)
├── openapi.yaml              # OpenAPI 3.0.3 contract for Choreo Gateway syncing
├── go.mod                    # Go module definition
├── go.sum
├── main.go                   # Clean service entry point
├── main_test.go              # Unit tests for main package helpers
├── README.md
└── internal/
    ├── auth/                 # JWT parsing and test token resolution
    │   ├── jwt.go
    │   └── jwt_test.go
    ├── buckets/              # PO_POLICIES capability bucket & tool schemas
    │   ├── policy.go
    │   └── policy_test.go
    ├── db/                   # pgvector repository layer
    │   └── db.go
    ├── embedding/            # Ingestion service embedding client
    │   ├── client.go
    │   └── client_test.go
    ├── handler/              # HTTP multiplexer, validation & search execution
    │   ├── handler.go
    │   └── handler_test.go
    └── model/                # Core domain and MCP schemas
        └── types.go
```

---

## 3. Configuration

Copy `.env.example` to `.env`:

```bash
cp .env.example .env
```

Key environment variables:
- **`PORT`**: Service port (default `8003`).
- **`DB_HOST`**, **`DB_PORT`**, **`DB_NAME`**, **`DB_USER`**, **`DB_PASSWORD`**, **`DB_SSL`**:
  PostgreSQL pgvector database connection.
- **`POLICY_DRIVE_FOLDER_ID`**: HR Policy Google Drive folder ID.
- **`TRAINING_DRIVE_FOLDER_ID`**: Training Google Drive folder ID.
- **`POLICY_INGESTION_URL`**: URL of the Policy Ingestion Service (e.g. `http://localhost:8080`) to generate
  query embeddings.
- **`TOKEN_VALIDATOR_ENABLED`**: If `true`, mandates cryptographic RSA RS256 signature verification via JWKS.
- **`CHOREO_JWKS_URL`**: JWKS endpoint for the Choreo API Gateway (Asgardeo validation is handled by Choreo).
- **`DEV_MOCK_USER_ENABLED`**: If `true`, permits mock user fallback for local testing in development environments.
- **`DEV_MOCK_EMAIL`**: Mock user email fallback (defaults to `user@example.com`).

---

## 4. Running & Testing Locally

### Run Unit Tests
```bash
go test -v ./...
```

### Start Server
```bash
go run main.go
```

### Health Check
```bash
curl http://localhost:8003/health
```

### Verify Tool Registration
```bash
curl http://localhost:8003/register
```

### Test Semantic Search
```bash
curl -X POST http://localhost:8003/tools/query_policy \
  -H "Content-Type: application/json" \
  -H "X-JWT-Assertion: test-token" \
  -d '{"query": "How many days of casual leave do I get per year?", "top_k": 3}'
```
