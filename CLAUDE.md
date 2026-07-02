# Husonym Architecture Overview

## Project Purpose

Husonym is an open-source, developer-first platform for **data anonymization and synthetic data orchestration**. It enables developers to:

- Anonymize production data for safe local testing
- Generate synthetic data for lower environments
- Subset production databases for debugging
- Maintain referential integrity and comply with GDPR, HIPAA, DPDP requirements

**Core tagline**: "Open Source Data Anonymization and Synthetic Data Orchestration"

---

## High-Level Architecture

Husonym consists of **three main components** that work together:

```
┌─────────────────────────────────────────────────────────────────┐
│                      FRONTEND (React/Next.js)                   │
│  - Job configuration UI                                          │
│  - Real-time job run monitoring                                  │
│  - Connect RPC Client calls to Backend                           │
└────────────────────────────┬────────────────────────────────────┘
                             │
                     Connect RPC (HTTP/2)
                             │
┌─────────────────────────────▼────────────────────────────────────┐
│              BACKEND (Go - Port 8080)                            │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │ Services (Connect RPC handlers):                           │  │
│  │ • JobService - Job lifecycle & execution                  │  │
│  │ • ConnectionService - Data source/dest connections        │  │
│  │ • TransformerService - Data transformation configs        │  │
│  │ • AnonymizationService - PII detection & anonymization    │  │
│  │ • AuthService - Authentication & authorization            │  │
│  │ • UserAccountService - Account management                 │  │
│  │ • MetricsService - Job run metrics                         │  │
│  │ • AccountHooksService - Async event handlers              │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                    │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │ Infrastructure:                                             │  │
│  │ • PostgreSQL/MySQL - Metadata & state storage              │  │
│  │ • Temporal Client - Job orchestration client               │  │
│  │ • gRPC Health Check / Reflection                           │  │
│  │ • RBAC & Licensing (EE features)                           │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────┬─────────────────────────────────────────┬──────────────┘
           │                                          │
    Temporal RPC                          Via Connect RPC
           │                                          │
┌──────────▼──────────────────┐           ┌──────────▼─────────────┐
│ TEMPORAL (Orchestrator)     │           │  WORKER (Go)           │
│ - Workflow definitions      │◄──────────│  - Data sync engine    │
│ - Activity scheduling       │ Register  │  - Transform execution │
│ - Retry policies            │ Workflows │  - Benthos integration │
│ - Job execution history     │           │  - Error handling      │
└─────────────────────────────┘           └─────────┬──────────────┘
                                                     │
                                    Connects to Data Sources
                                                     │
                ┌────────────────────────┬──────────┴──────┬─────────┐
                │                        │                  │         │
         ┌──────▼────────┐    ┌─────────▼─┐    ┌──────────▼──┐  ┌──┴────┐
         │  PostgreSQL   │    │   MySQL   │    │  Databases  │  │ S3    │
         │   DynamoDB    │    │ MongoDB   │    │   & APIs    │  │ GCS   │
         └───────────────┘    └───────────┘    └─────────────┘  └───────┘
```

---

## Component Deep-Dive

### 1. **Backend Service** (`/workspaces/husonym/backend`)

The backend is a **Connect RPC (gRPC-web compatible) service** written in Go.

#### Key Technologies:
- **Framework**: [connectrpc.com/connect](https://connectrpc.com) - Modern alternative to traditional gRPC
- **Protocol Buffers**: `.proto` files define service contracts (`/backend/protos/mgmt/v1alpha1/`)
- **Database**: PostgreSQL (primary), MySQL (optional) via sqlc for type-safe queries
- **HTTP Server**: HTTP/2 with h2c (HTTP/2 Cleartext) for Connect support
- **ORM Pattern**: Uses generated query functions from `.sql` files (sqlc-based)

#### Core Services Exposed:

| Service | Purpose |
|---------|---------|
| **JobService** | Create, update, run jobs; manage job runs and logs |
| **ConnectionService** | Manage data source/destination connections |
| **TransformerService** | Configure and retrieve data transformers |
| **AnonymizationService** | Real-time PII anonymization; calls Presidio API |
| **UserAccountService** | User/account management, billing integration |
| **AuthService** | JWT/API key auth; CLI login flows |
| **ConnectionDataService** | Introspect schemas, get init statements |
| **MetricsService** | Query Prometheus for job metrics |
| **AccountHookService** | (EE) Event-driven webhooks & triggers |

#### Key Flow - Job Execution:

```
1. Frontend calls JobService.CreateJobRun(job_id)
   ↓
2. Backend calls Temporal Client Manager to start workflow
   ↓
3. Temporal spawns datasync_workflow (main orchestrator)
   ↓
4. Worker processes the workflow (covered below)
```

#### Database Schema:
- **Migrations**: `/backend/sql/postgresql/schema/` - Version-controlled SQL
- **Querying**: Using sqlc (type-safe SQL in Go)
- **Tables**: jobs, connections, transformers, job_mappings, run_contexts, job_hooks, etc.

#### Interceptors (Middleware):
- `auth_interceptor` - JWT/API key validation
- `logger_interceptor` - Structured logging
- `accountid_interceptor` - Extract account context
- `otelconnect` - OpenTelemetry tracing
- `validate` - Protobuf validation

---

### 2. **Worker Service** (`/workspaces/husonym/worker`)

The worker **executes Temporal workflows** that perform the actual data synchronization.

#### Key Technologies:
- **Temporal SDK**: `go.temporal.io/sdk` - Orchestrates workflows & activities
- **Benthos**: `redpanda-data/benthos` - Stream processing engine for ETL
- **Bloblang**: Benthos query language for data transformation

#### Workflow Architecture: `datasync_workflow`

The main workflow orchestrates multi-table synchronization with dependency management:

```
datasync_workflow (Workflow)
├── CheckAccountStatus (Activity)
│   └─ Verify account is valid; poll during sync
├── GenerateBenthosConfigs (Activity)
│   └─ Build Benthos configs for each table based on job config
├── RunJobHooksByTiming (Activity) [PRE_SYNC]
│   └─ Execute SQL hooks before data sync
├── SchemaInitWorkflow (Child Workflow)
│   └─ For each destination: create schema/tables
├── TableSync Concurrency Loop (with max concurrency)
│   ├── Root tables first (no dependencies)
│   └── Dependent tables (after their FK tables complete)
├── RunJobHooksByTiming (Activity) [POST_SYNC]
│   └─ Execute SQL hooks after data sync
└── DeleteRedisHash (Activity)
    └─ Clean up temporary Redis state
```

**Key Characteristics**:
- **Concurrent execution** with configurable max concurrency (default: 3 tables)
- **Dependency tracking** for tables with foreign keys
- **Graceful shutdown** - cancellation on account status change
- **Activity retry policies** - per-activity timeout/retry config
- **Heartbeats** - long-running activities send heartbeats to prevent timeout

#### Benthos Integration

Benthos handles the actual **data reading, transformation, and writing**:

```
Benthos Config Structure:
├── Input
│   ├── Postgres/MySQL/MongoDB reader
│   └── WHERE clause for subsetting
├── Pipeline
│   ├── Transformers (anonymize, generate, passthrough)
│   ├── Type conversions
│   └── Reference integrity checks
└── Output
    └── Destination writer (Postgres/MySQL/S3/MongoDB/etc)
```

**Generated Benthos configs** include:
- Source queries with column mappings
- Transformer assignments per column
- Destination insert/upsert logic
- Referential integrity joins
- Batching configuration

#### Transformers

Column-level transformers applied during sync:

| Category | Examples |
|----------|----------|
| **Generate** | `GenerateFirstName`, `GenerateEmail`, `GenerateUUID`, `GeneratePhoneNumber` |
| **Transform** | `TransformPhoneNumber`, `TransformEmail`, `TransformInt64`, `TransformString` |
| **PII** | `TransformPiiText` (calls AnonymizationService via Connect RPC) |
| **Passthrough** | Keep original data (for non-sensitive columns) |

Each transformer is a **Bloblang function** registered with Benthos.

---

### 3. **Frontend Application** (`/workspaces/husonym/frontend`)

React-based UI for job configuration and monitoring.

#### Technologies:
- **Framework**: Next.js 15 (React 19)
- **Styling**: Tailwind CSS + Radix UI components
- **State**: Zustand (lightweight state management)
- **API Client**: 
  - `@connectrpc/connect-web` - Connect RPC client (browser compatible)
  - `@connectrpc/connect-query` - React Query integration
  - Protobuf serialization via `@bufbuild/protobuf`
- **Forms**: React Hook Form + Yup validation
- **Charting**: Recharts for metrics visualization
- **Auth**: NextAuth.js with external providers (Auth0/Keycloak)

#### Key Features:
- **Job Builder**: Configure source → mappings → destinations
- **Transformer Selection**: Per-column PII/synthetic config
- **Job Runs**: Monitor live execution with logs & metrics
- **Connections**: Add/test data connections
- **CLI Auth**: OAuth flow for CLI login

#### Data Flow (Browser → Backend):
```
React Component
  ↓
useQuery/useMutation (Connect Query)
  ↓
Connect RPC Client (HTTP/2, Protobuf)
  ↓
Backend Service Handler
  ↓
Response deserialized to React state
```

---

### 4. **CLI** (`/workspaces/husonym/cli`)

Command-line interface for automation and local development.

#### Commands:
- `husonym login` - Authenticate via OAuth flow
- `husonym job <command>` - List, create, execute jobs
- `husonym connection <command>` - Manage connections
- `husonym transform <command>` - Test transformers

#### Architecture:
- Uses Connect RPC to communicate with backend
- Stores auth tokens in local config (`~/.husonym/`)
- Supports API key auth for CI/CD pipelines

---

## Data Flow: End-to-End Job Execution

### Scenario: User runs a data sync job

```
1. FRONTEND
   User clicks "Run Job"
   → Frontend calls Backend: JobService.CreateJobRun(job_id)

2. BACKEND (HTTP Handler)
   POST /mgmt.v1alpha1.JobService/CreateJobRun
   → Validates auth & permissions (interceptors)
   → Loads job config from DB
   → Creates Temporal workflow client
   → Calls temporal_client.ExecuteWorkflow(datasync_workflow, request)
   → Returns immediately with job_run_id

3. TEMPORAL SERVER
   Receives workflow start request
   → Schedules datasync_workflow on worker pool
   → Returns workflow_execution_id

4. WORKER (Receives workflow)
   datasync_workflow starts:
   
   4a. CheckAccountStatus Activity
       → Queries backend via API
       → Confirms account is active
       → Sets up polling for account status
   
   4b. GenerateBenthosConfigs Activity
       → Calls backend to get job config
       → For each table in job mapping:
         - Generate source query with WHERE clause
         - Map columns to transformers
         - Generate destination insert config
       → Returns Benthos configs in YAML/JSON
   
   4c. SchemaInitWorkflow (Child)
       → For each destination:
         - Connect to DB
         - Create schema if needed
         - Create/alter tables based on source
   
   4d. TableSync Loop
       → Spawn table1, table2, table3 (up to max concurrency)
       → For each table:
         - Create Benthos process
         - Execute: read from source + transform + write to dest
         - Wait for completion
         - Execute post-sync activities (indexes, triggers)
       → Queue table4, table5 when table1 completes
       → Manage FK dependencies
   
   4e. Post-sync hooks
       → Execute any configured SQL hooks
   
   4f. Cleanup
       → Delete Redis temporary state
       → Workflow completes

5. DATA TRANSFORMATION (in Benthos)
   Source Row: { id: 1, email: "user@example.com", phone: "555-1234" }
   
   Transformers applied:
   - email: passthrough (already anonymized at source)
   - phone: TransformPhoneNumber(GeneratePhoneNumber)
   - pii_field: TransformPiiText(call AnonymizationService)
   
   Destination Row: { id: 1, email: "user@example.com", phone: "555-0000" }

6. FRONTEND (Polling)
   → Calls JobService.GetJobRun(job_run_id) every 2-5 seconds
   → Calls JobService.GetJobRunEvents(job_run_id) for activity details
   → Calls JobService.GetJobRunLogs(job_run_id) for worker logs
   → UI updates in real-time

7. COMPLETION
   → Temporal marks workflow as complete
   → Metrics recorded in Prometheus
   → Events sent to AccountHooks if configured
```

---

## Key Architectural Patterns

### 1. **Event Sourcing Lite**
- **Job runs** stored as event history in database
- **Events** captured for each activity (schema init, table sync, hooks)
- **Temporal event history** provides complete audit trail
- **Run contexts** can store state for continuation/recovery

### 2. **gRPC/Connect RPC Communication**
- **Backend ↔ Frontend**: Connect RPC (HTTP/2, Protobuf)
- **Backend ↔ Worker**: Temporal RPC (internal)
- **Worker ↔ Backend**: Connect RPC for callbacks (get job config, validate account)
- **Worker ↔ Anonymization**: Connect RPC to call AnonymizationService

### 3. **Workflow as Code** (Temporal)
- Workflows are pure Go functions
- Activities are side-effecting operations (I/O, DB calls)
- Deterministic replay for fault tolerance
- Retries & timeouts configured per activity

### 4. **Benthos Pipeline**
- Declarative ETL: source → pipeline → output
- Column-level transformers composable
- Streaming (batched) vs. single-row modes
- Supports multiple DB types seamlessly

### 5. **Dependency Management**
- Foreign key constraints analyzed
- Tables with dependencies queued after prerequisites
- Redis used as temporary state store for FK tracking
- Circular dependency detection

### 6. **Multi-Tenancy**
- Account ID extracted from auth tokens (interceptor)
- All queries scoped by `account_id`
- Isolation at DB level via WHERE clauses
- RBAC (EE feature) enforced per account

---

## Temporal Workflows Deep-Dive

### Why Temporal?
- **Durability**: Workflows survive process crashes
- **Retry logic**: Built-in with backoff policies
- **Async execution**: Non-blocking job orchestration
- **Cron scheduling**: Jobs can run on schedule
- **Event history**: Complete audit trail
- **Visibility**: Dashboard for monitoring

### Workflow Concepts in Husonym:

| Concept | Example in Husonym |
|---------|-------------------|
| **Workflow** | `datasync_workflow` - Main job orchestrator |
| **Activity** | `GenerateBenthosConfigs` - Single unit of work |
| **Child Workflow** | `SchemaInitWorkflow` - Nested workflow per destination |
| **Selector** | Concurrent table sync loop with dependency checks |
| **Retry Policy** | Max attempts = 2 for account status check |
| **Timeout** | StartToCloseTimeout = 5 min for benthos generation |

---

## Frontend Communication Pattern

### Protocol: Connect RPC

```javascript
// Frontend code example
import { useQuery } from '@connectrpc/connect-query';
import { JobService } from '@husonym/sdk/proto';

export function JobRunMonitor({ jobId }) {
  const { data: jobRun } = useQuery(
    JobService.method.GetJobRun, 
    { jobRunId: jobId, accountId: accountId }
  );
  
  // Re-renders automatically when backend response updates
  return <div>Status: {jobRun.status}</div>;
}
```

**Network flow**:
1. Serializes request to Protobuf binary
2. HTTP POST to `/mgmt.v1alpha1.JobService/GetJobRun`
3. Backend deserializes, processes, serializes response
4. Frontend deserializes to typed JavaScript object
5. React Query caches & triggers re-render

---

## Database Schema Overview

### Key Tables:
- **jobs** - Job definitions & configurations
- **connections** - Data source/destination credentials
- **job_mappings** - Column-level transformation configs
- **transformers** - Transformer definitions (built-in & custom)
- **job_runs** - Job execution records
- **run_contexts** - Workflow continuation state
- **job_hooks** - Pre/post-sync SQL hooks
- **account_hooks** - (EE) Event-driven actions

### Migrations:
- Version-controlled in `/backend/sql/postgresql/schema/`
- Timestamp-based naming: `20240822000658_adds-run-context.up.sql`
- Support for PostgreSQL and MySQL

---

## Technologies Stack Summary

| Layer | Tech | Purpose |
|-------|------|---------|
| **Frontend** | Next.js, React, Tailwind, Zustand | UI & state management |
| **API** | Connect RPC, Protobuf | Type-safe backend communication |
| **Backend** | Go, PostgreSQL, sqlc | Business logic & data persistence |
| **Orchestration** | Temporal | Workflow scheduling & durability |
| **Worker** | Go, Benthos, Bloblang | Data synchronization engine |
| **Auth** | JWT, API keys, OAuth | Security & multi-tenancy |
| **Observability** | OpenTelemetry, Prometheus, Loki | Tracing, metrics, logs |

---

## Key Design Decisions

### Why Temporal?
- Job runs must be reliable across network failures
- Complex multi-step processes need retry logic
- Audit trail required for compliance (GDPR)

### Why Benthos?
- Stream processing library handles complex ETL
- Supports multiple source/destination types
- Composable transformer pipeline
- Native Bloblang for data manipulation

### Why Connect RPC?
- Modern alternative to gRPC (gRPC-web compatible)
- Smaller payloads than JSON APIs
- Type-safe schema (Protobuf)
- Works in browsers (unlike gRPC/2 in many cases)

### Why Multi-Component?
- **Separation of concerns**: Backend handles API, Worker handles compute
- **Independent scaling**: Workers can scale without backend load
- **Resource isolation**: Heavy workloads don't impact API responsiveness

---

## Example: Anonymizing a Single Column

```
Frontend Config: "anonymize email with TransformPhoneNumber"
  ↓
Backend stores: JobMapping { 
  schema: "public", 
  table: "users", 
  column: "email",
  transformer: { GenerateEmail {} }
}
  ↓
Worker receives config in GenerateBenthosConfigs:
  - Reads transformer definition
  - Generates Benthos pipeline: .email |= generate_email()
  ↓
During sync (Benthos execution):
  - For each row: extract .email column
  - Pass through GenerateEmail transformer
  - Transform anonymized → new_generated_email
  - Write to destination
  ↓
Result: Original email replaced with synthetic email
```

---

## Event Sourcing Pattern

### Job Lifecycle Events:
1. **JOB_RUN_CREATED** - Workflow starts
2. **ACTIVITY_STARTED** - Each major step begins
3. **ACTIVITY_COMPLETED** - Step finishes
4. **JOB_RUN_SUCCEEDED** - All steps done
5. **JOB_RUN_FAILED** - Error occurred

### Storage:
- **Temporal event history** - Immutable sequence of events
- **Database records** - Denormalized for query efficiency
- **Run contexts** - Mutable state for continuation

---

## Extension Points

### Custom Transformers:
- JavaScript-based: Users write custom Bloblang functions
- LLM-based: Call OpenAI to generate synthetic data
- API-based: Call external service for transformation

### Data Connections:
- Add new connection type in `ConnectionService`
- Implement builder pattern for Benthos config generation
- Register in connection manager

### Job Hooks:
- Pre-sync: Data prep, validation
- Post-sync: Cleanup, verification, notifications

---

## Compliance & Security

### Multi-Tenancy:
- Account ID in auth token
- Scoped queries via `WHERE account_id = ?`
- API key isolation

### PII Handling:
- Presidio integration for detection
- Never stores raw sensitive data in logs
- Encrypted storage for credentials

### Audit Trail:
- All job runs recorded
- Events stored immutably in Temporal
- Who ran what, when, with what result

### Licensing (EE):
- RBAC enforcement
- Cloud licensing validation
- Feature gates for account hooks, metrics, etc.

