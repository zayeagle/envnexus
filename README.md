# EnvNexus

[中文版本](README.zh-CN.md)

**EnvNexus** is an AI-driven **Enterprise Intelligent Endpoint Execution & Remediation Platform**. It aims to transform dangerous Root/Admin privileges into safe **natural language to controlled tool execution**, solving the traditional IT operations problems of error-prone command typing, excessive permissions, and lack of auditability.

Users simply describe their intent in natural language, or administrators issue exact commands, and the EnvNexus Agent automatically translates them into structured execution plans. The system follows a **tool-first execution funnel**, strictly limiting the use of raw Shell commands, and combines **layered approvals** with **automatic rollbacks** to ensure every endpoint change is safe, controlled, and auditable. Additionally, it provides **intelligent diagnosis and proactive remediation** for environmental anomalies, as well as **controlled remote file system access** for logs and evidence collection.

---

## Product Screenshots

<p align="center">
  <img src="docs/images/console-dashboard.png" alt="Console Dashboard" width="720" />
</p>
<p align="center"><em>Console Dashboard — devices, sessions, and recent activity at a glance</em></p>

<p align="center">
  <img src="docs/images/console-command-tasks.png" alt="Natural Language Command Tasks" width="720" />
</p>
<p align="center"><em>Command Tasks — natural language intent translated into a structured, tool-first execution plan</em></p>

<p align="center">
  <img src="docs/images/console-approvals.png" alt="Layered Approvals" width="720" />
</p>
<p align="center"><em>Pending Approvals — L0–L3 risk gates for commands and file access</em></p>

<p align="center">
  <img src="docs/images/console-devices.png" alt="Device Health" width="720" />
</p>
<p align="center"><em>Device Health — online/offline fleet status and agent versions</em></p>

<p align="center">
  <img src="docs/images/agent-desktop-chat.png" alt="Agent Desktop Diagnosis" width="720" />
</p>
<p align="center"><em>Agent Desktop — chat-based diagnosis with planned remediation and plan approval</em></p>

---

## The Problem It Solves

In modern enterprise IT management, endpoint execution faces a huge contradiction between safety and efficiency: ordinary business users cannot type commands and must wait for IT remote control when issues arise; meanwhile, IT operations typing commands for troubleshooting or batch management are prone to errors, hold excessive permissions, and lack structured auditing.

EnvNexus provides a safer, more intelligent closed-loop for endpoint execution:

- **Natural Language to Controlled Execution (Core)** — Users describe their intent in natural language, and the Agent automatically translates it into a structured execution plan, allowing non-technical personnel to safely perform daily maintenance.
- **Controlled Dispatch of Exact Commands (Core)** — Administrators can dispatch exact commands. The system wraps them into execution plans and triggers high-level approvals, replacing traditional high-risk script broadcasting.
- **Tool-First Execution Funnel** — Strictly limits raw Shell. Prioritizes safe built-in structured tools, only degrading to Shell commands when no tool is available, which triggers the highest level of approval.
- **Layered Approvals & Auto-Rollback** — L0 auto-pass, L1 plan-level approval, L2 plan + confirmation, L3 step-by-step approval; snapshots are taken before each step, with automatic rollback on failure.
- **Intelligent Diagnosis & Planned Remediation (Secondary)** — For complex local environment faults (e.g., network, proxy, database, containers), AI automatically collects evidence, analyzes root causes, and generates remediation plans (DAGs).
- **Controlled File Viewing & Downloading** — Supports browsing remote endpoint file systems, previewing text logs, and downloading raw files of any format after approval, typically used for log troubleshooting and evidence collection.
- **Proactive Discovery & Watchlist (Secondary)** — Users can define focus items in natural language. The Agent automatically breaks them down and continuously inspects them, alerting before faults erupt.
- **Full-Link Auditing** — Every session, diagnosis, plan, approval, execution, file access, and verification is recorded and queryable.
- **Failsafe OTA Self-Update** — Off by default. When enabled, it uses a "cloud-side grayscale policy + staggered endpoint pulling + auto-rollback on failure" mechanism to avoid "network explosions."

### What EnvNexus is NOT

- NOT an unrestricted Shell tool capable of executing arbitrary dangerous commands
- NOT a traditional remote desktop takeover software
- NOT a bloated, all-in-one unified endpoint management platform
- Will NOT bypass local policies—the local Agent always has the final say

---

## Architecture

```text
┌──────────────────────────────────────────────────────────────┐
│                       Platform Side (Server)                   │
│                   (Docker Compose / K8s)                      │
│                                                              │
│   console-web ──> platform-api ──> session-gateway           │
│   (Next.js 14)    (Go / Gin)       (Go / WebSocket)         │
│      :3000          :8080             :8081                   │
│                       │                                      │
│              ┌────────┼────────┐                             │
│           MySQL    Redis    MinIO        job-runner           │
│                                          (7 Workers)         │
│                                                              │
│                    Feishu / Lark Bot (Conversational)         │
└──────────────────────────┬───────────────────────────────────┘
                           │ HTTPS / WSS
┌──────────────────────────┼───────────────────────────────────┐
│                      Endpoint Side (Client)                    │
│                                                              │
│   agent-desktop (Electron 30) ──IPC──> agent-core (Go)       │
│   - System Tray, Chat UI,               - LLM Router (7 Providers)│
│     Plan Approval, Watchlist,           - 33+ Structured Tools  │
│     Health Dashboard                    - Execution Plan Gen   │
│                                        - Diagnosis Engine     │
│                                        - Watchlist Engine     │
│                                        - Governance Engine    │
│                                        - SQLite Local Storage │
│                                        - OTA Updater          │
└──────────────────────────────────────────────────────────────┘
```

**Platform Side**: Multi-tenant control plane containing the management console, REST API, WebSocket gateway, and background job scheduling. Infrastructure relies on MySQL, Redis, and MinIO.

**Endpoint Side**: A local execution kernel (`agent-core`) written in Go running on managed devices, paired with an Electron desktop UI. The kernel handles AI intent parsing (supporting 7 LLM providers), execution plan generation, 33+ structured tool executions, policy interception, diagnosis, watchlist inspection, governance (baseline collection + drift detection), and an offline degradation mode.

**Integrations**: Feishu/Lark conversational bot—after binding a group chat to a device, users can trigger execution or diagnosis via natural language, with real-time progress pushes and in-group approval cards.

---

## Core Design Decisions

| Domain | Approach |
|--------|----------|
| **Security** | Tool-first execution funnel; strict limits on raw Shell; all write ops require approval; 4-tier JWT token system; AES-256-GCM local config encryption |
| **Multi-tenancy** | DB, cache, storage, and audits are all tenant-isolated |
| **AI Integration** | LLM Router supports 7 providers (OpenAI, Anthropic, DeepSeek, Gemini, OpenRouter, Ollama, Local); structured output; tool-calling mode |
| **Resilience** | Graceful degradation when Agent is offline (read-only tools + local SQLite); fallback to local FS if MinIO is unavailable |
| **Access Control** | RBAC with 5 preset roles + 17 permissions; IP-based rate limiting |
| **Deployment** | Docker Compose, Helm Chart, or bare metal; on-premise ready (no cloud dependencies) |

---

## Quick Start

**Prerequisites**: Docker 20.10+, Docker Compose v2

```bash
git clone https://github.com/zayeagle/envnexus.git
cd envnexus
./deploy.sh start
```

The script automatically detects the host IP, generates keys, calculates source hashes to rebuild only changed services, and starts all components with one click.

**Default Access**: `http://localhost:3000` — Login: `admin@envnexus.io` / `admin123`

For more deployment methods, see [Deployment Options](#deployment-options).

---

## Deployment Options

| Method | Command | Use Case |
|--------|---------|----------|
| **Smart Deploy** (Rec) | `./deploy.sh start` | One-click deploy with auto change detection |
| **Full Rebuild** | `./deploy.sh full` | Force rebuild all services |
| **Docker Compose** | `cd deploy/docker && docker compose up -d` | Manual control |
| **Kubernetes** | `helm install envnexus deploy/k8s/helm/envnexus ...` | Production K8s cluster |
| **Local Dev** | `make build` + run binaries | Go dev mode without Docker |

Default ports: console-web `:3000`, platform-api `:8080`, session-gateway `:8081`, job-runner `:8082`, agent-core `:17700` (localhost only).

---

## Repository Structure

```text
envnexus/
├── apps/
│   ├── console-web/        # Next.js 14 Management Console
│   ├── agent-desktop/      # Electron 30 Desktop UI
│   └── agent-core/         # Go Local Execution Kernel
├── services/
│   ├── platform-api/       # Go Core API (Gin + GORM, DDD)
│   ├── session-gateway/    # Go WebSocket Gateway
│   └── job-runner/         # Go Background Jobs
├── libs/shared/            # Go Shared Libraries
├── deploy/
│   ├── docker/             # Docker Compose configs
│   └── k8s/helm/envnexus/ # Kubernetes Helm Chart
├── scripts/                # Smoke tests, init data
├── docs/                   # User manuals, whitepapers
└── Makefile
```

---

## Project Status

All core modules are fully functional (Phases 0–6 implemented):

| Module | Status | Highlights |
|--------|--------|------------|
| Platform API | Done | Auth, RBAC, Webhooks, Usage Metrics, License, Lark Integration |
| Session Gateway | Done | WS Relay, Event Deduplication, Redis pub/sub horizontal scaling |
| Job Runner | Done | 7 Workers, Atomic Job Preemption, Audit Archiving |
| Agent Core | Done | 7 LLMs, 33+ Tools, Exec Plan Gen, Diagnosis Engine, Governance, OTA |
| Console Web | Done | 12+ Pages, i18n (EN/ZH), Unified API Client |
| Agent Desktop | Done | Tray, Chat UI, Approval Management, Auto-update |
| Lark Integration | Done | Conversational execution/diagnosis, real-time push, approval cards |
| Infrastructure | Done | 25 Tables, Helm Chart, Docker Compose |

### Known Limitations

- **Testing**: Insufficient Integration/E2E test coverage; no OpenAPI spec; no performance benchmarks.
- **Enterprise Features**: No SSO (LDAP/SAML/OIDC); no billing/metering; LLM prompts are English-only.
- **Observability**: No distributed tracing (OpenTelemetry); no Prometheus/Grafana; DB polling for job queues.
- **Ecosystem**: Only Feishu/Lark integration (no Slack/Teams); no runtime plugin loading.

---

## Documentation

- **User Manual**: [`docs/user-manual.md`](docs/user-manual.md) — Guide for end-users and administrators
- **Product Whitepaper**: [`docs/product-manual.md`](docs/product-manual.md) — Product positioning and commercial solutions

## License

See [`LICENSE`](LICENSE).
