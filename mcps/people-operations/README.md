# People Operations MCP Server

Domain Model Context Protocol (MCP) server for WSO2 People Operations tools.
Provides domain-specific tools for leave management, cafeteria menus, campus vehicle parking, office locations, official HR letters, and meal/commute subscriptions.

Registers with the **Agent Orchestrator** via the standard HTTP REST Tool Protocol:
- `GET /health`: Health probe.
- `GET /register`: Advertises the registered `BucketKit` list and tool schemas.
- `POST /tools/{tool_name}`: Proxies tool execution to downstream WSO2 microservices with Asgardeo Token Relay.

---

## 1. Registered Buckets & Tools

| Bucket | Domain | Tools | Description |
| :--- | :--- | :--- | :--- |
| **`LEAVE_STANDARD`** | People Operations | `validate_leave`, `submit_leave`, `get_leave_history`, `get_pending_leaves`, `cancel_leave`, `check_calendar` | Casual, sick, and annual leave management. |
| **`PO_VEHICLES`** | People Operations | `get_vehicles`, `add_vehicle`, `delete_vehicle` | Campus vehicle registration and parking. |
| **`PO_EMPLOYEE`** | People Operations | `get_my_location`, `fetch_leads` | Employee office and workplace location queries. |
| **`PO_LETTERS`** | People Operations | `get_letter_request_form` | Official HR letter requests. |
| **`PO_MENU`** | People Operations | `get_menu`, `submit_lunch_feedback` | Cafeteria menus and lunch meal feedback. |
| **`PO_SUBSCRIPTIONS`** | People Operations | `get_lunch_subscription`, `subscribe_lunch`, `unsubscribe_lunch`, `get_commute_subscription`, `subscribe_commute`, `unsubscribe_commute` | Lunch as a Service (LaaS) and PickMe Commute subscriptions. |

---

## 2. Directory Layout

```
mcps/people-operations/
├── .choreo/
│   ├── component.yaml        # Choreo REST endpoint configuration (port 8080)
│   └── openapi.yaml          # OpenAPI contract for Choreo Gateway
├── .dockerignore
├── .env.example              # Configuration template
├── .gitignore                # Ignores compiled binaries and secrets
├── Dockerfile                # Multi-stage non-root container for Choreo
├── README.md                 # Architecture and service documentation
├── go.mod                    # Go module definition
├── go.sum
├── main.go                   # Server initialization and routing
├── internal/
│   ├── auth/                 # Identity token extraction and claim parsing
│   ├── buckets/              # Modular bucket definitions and tool schemas
│   ├── client/               # Downstream backend HTTP clients
│   ├── handler/              # HTTP handlers for register, health, and tools
│   └── model/                # Core domain and schema types
└── tests/                    # Automated unit and integration tests
```

---

## 3. Configuration

Key environment variables:
- **`PORT`**: Service port (default `8080`).
- **`LEAVE_BACKEND_URL`**: Backend URL for Leave and Calendar APIs.
- **`PEOPLE_BACKEND_URL`**: Backend URL for Employee Profile and Vehicle APIs.
- **`MENU_BACKEND_URL`**: Backend URL for Cafeteria Menu and Meal Feedback APIs.
- **`SUBSCRIPTION_BACKEND_URL`**: Backend URL for Lunch (LaaS) and Commute APIs.
- **`LETTER_REQUEST_FORM_URL`**: URL of the official PO letter request form.

---

## 4. Running & Testing Locally

### Start Server
```bash
go run main.go
```

### Health Check
```bash
curl http://localhost:8080/health
```

### Discovery & Tool Registration
```bash
curl http://localhost:8080/register
```

### Run Automated Tests
```bash
go test -v ./...
```
