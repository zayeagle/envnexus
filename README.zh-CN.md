# EnvNexus

[English Version](README.md)

**EnvNexus** 是一个由 AI 驱动的**企业级智能终端执行与自愈平台**。它致力于将危险的 Root/Admin 权限转化为安全的**自然语言转受控工具执行**，解决传统运维敲命令易出错、权限过大且难以审计的问题。

用户只需通过自然语言下发意图，或由管理员下发确切命令，EnvNexus 的 Agent 即可自动将其转化为结构化的执行计划。系统遵循**工具优先的执行漏斗**，严格限制原生 Shell 的使用，并配合**分层审批**与**自动回滚**机制，确保每一次终端变更安全、可控、可审计。在此基础上，它还提供环境异常的**智能诊断与主动自愈**能力，以及**受控的远程文件系统查看与下载**功能。

---

## 产品截图

<p align="center">
  <img src="docs/images/console-dashboard.png" alt="控制台仪表盘" width="720" />
</p>
<p align="center"><em>控制台仪表盘 — 设备、会话与最近活动一览</em></p>

<p align="center">
  <img src="docs/images/console-command-tasks.png" alt="自然语言命令任务" width="720" />
</p>
<p align="center"><em>命令任务 — 自然语言意图翻译为工具优先的结构化执行计划</em></p>

<p align="center">
  <img src="docs/images/console-approvals.png" alt="分层审批" width="720" />
</p>
<p align="center"><em>待审批 — 命令与文件访问的 L0–L3 风险闸门</em></p>

<p align="center">
  <img src="docs/images/console-devices.png" alt="设备健康" width="720" />
</p>
<p align="center"><em>设备健康 — 在线/离线舰队状态与 Agent 版本</em></p>

<p align="center">
  <img src="docs/images/agent-desktop-chat.png" alt="Agent 桌面端诊断" width="720" />
</p>
<p align="center"><em>Agent 桌面端 — 对话式诊断、计划式修复与计划审批</em></p>

---

## 解决什么问题

在现代企业 IT 管理中，终端执行面临着巨大的安全与效率矛盾：普通业务人员不会敲命令，遇到问题只能等 IT 远控；而 IT 运维在排障或批量管理时敲命令，又容易出错、权限过大且缺乏结构化审计。

EnvNexus 提供的是一套更安全、智能的终端执行闭环：

- **自然语言转受控执行（核心）** — 用户通过自然语言描述意图，Agent 自动翻译为结构化执行计划，让缺乏技术背景的人员也能安全执行日常维护。
- **确切命令的受控下发（核心）** — 管理员可下发确切命令，系统将其包装为执行计划并触发高级别审批，替代传统高危的脚本群发。
- **工具优先的执行漏斗** — 严格限制原生 Shell。优先调用安全的内置结构化工具，仅在无工具可用时才降级为 Shell 命令，并触发最高级别审批。
- **分层审批与自动回滚** — L0 自动通过、L1 计划级审批、L2 计划+确认、L3 逐步审批；每个修复步骤执行前快照，失败自动回滚。
- **智能诊断与计划式修复（副线）** — 针对复杂的本地环境故障（如网络、代理、数据库、容器），AI 自动采集证据、分析根因并生成修复方案（DAG）。
- **受控文件查看与下载** — 支持在审批后浏览远程终端文件系统、预览文本日志、下载任意格式的原始文件，典型用于日志排障与证据采集。
- **主动发现与 Watchlist（副线）** — 用户可用自然语言定义关注项，Agent 自动拆解并持续巡检，在故障爆发前预警。
- **全量审计** — 每个会话、诊断、计划、审批、执行、文件访问和验证都被记录并可查询。
- **防失联的灰度自更新（OTA）** — 默认关闭。开启后采用“云端灰度策略下发 + 终端错峰拉取 + 失败自动回滚”机制，避免“炸网”。

### EnvNexus 不是什么

- 不是可以执行任意危险命令的无限制 Shell 工具
- 不是传统的远程桌面接管软件
- 不是大而全的统一终端管理平台
- 不会绕过本地策略——本地 Agent 始终拥有最终决定权

---

## 架构

```text
┌──────────────────────────────────────────────────────────────┐
│                       平台侧（服务端）                         │
│                   (Docker Compose / K8s)                      │
│                                                              │
│   console-web ──> platform-api ──> session-gateway           │
│   (Next.js 14)    (Go / Gin)       (Go / WebSocket)         │
│      :3000          :8080             :8081                   │
│                       │                                      │
│              ┌────────┼────────┐                             │
│           MySQL    Redis    MinIO        job-runner           │
│                                          (7 个 Worker)       │
│                                                              │
│                    飞书 / Lark Bot（对话式集成）                │
└──────────────────────────┬───────────────────────────────────┘
                           │ HTTPS / WSS
┌──────────────────────────┼───────────────────────────────────┐
│                      终端侧（客户端）                          │
│                                                              │
│   agent-desktop (Electron 30) ──IPC──> agent-core (Go)       │
│   - 系统托盘、诊断对话、                - LLM Router（7 家）    │
│     计划审批、关注点管理、              - 33+ 结构化工具        │
│     健康仪表盘                          - 执行计划生成器       │
│                                        - 诊断与修复引擎       │
│                                        - 关注点巡检引擎       │
│                                        - 治理引擎             │
│                                        - SQLite 本地存储      │
│                                        - OTA 自更新器         │
└──────────────────────────────────────────────────────────────┘
```

**平台侧**：多租户控制面，包含管理控制台、REST API、WebSocket 网关和后台任务调度。基础设施依赖 MySQL、Redis 和 MinIO。

**终端侧**：Go 编写的本地执行内核（`agent-core`）运行在受管设备上，配合 Electron 桌面 UI。内核负责 AI 意图解析（支持 7 家 LLM Provider）、执行计划生成、33+ 结构化工具执行、策略拦截、诊断引擎、关注点巡检（Watchlist）、治理（基线采集 + 漂移检测）以及离线降级模式。

**集成**：飞书对话式 Bot——将群聊绑定到设备后，通过自然语言触发执行或诊断，实时推送进度和群内审批卡片。

---

## 核心设计决策

| 领域 | 方案 |
|------|------|
| **安全** | 工具优先的执行漏斗；严格限制原生 Shell；所有写操作需审批；4 层 JWT 令牌体系；AES-256-GCM 本地配置加密 |
| **多租户** | 数据库、缓存、存储、审计均按租户隔离 |
| **AI 集成** | LLM Router 支持 7 家 Provider（OpenAI、Anthropic、DeepSeek、Gemini、OpenRouter、Ollama、本地模型）；结构化输出；tool-calling 模式 |
| **韧性** | Agent 离线时优雅降级（只读工具 + 本地 SQLite）；MinIO 不可用时回退到本地文件系统 |
| **访问控制** | RBAC 5 种预置角色 + 17 条权限；按 IP 限频 |
| **部署** | Docker Compose、Helm Chart 或裸机部署；私有化就绪（无云依赖） |

---

## 快速开始

**前置条件**：Docker 20.10+、Docker Compose v2

```bash
git clone https://github.com/zayeagle/envnexus.git
cd envnexus
./deploy.sh start
```

脚本自动检测宿主机 IP、生成密钥、计算源码 hash 仅重建有变更的服务，一键启动全部组件。

**默认访问**：`http://localhost:3000` — 登录：`admin@envnexus.io` / `admin123`

更多部署方式参见[部署选项](#部署选项)。

---

## 部署选项

| 方式 | 命令 | 适用场景 |
|------|------|---------|
| **智能部署**（推荐） | `./deploy.sh start` | 一键部署，自动变更检测 |
| **全量重建** | `./deploy.sh full` | 强制重建所有服务 |
| **Docker Compose** | `cd deploy/docker && docker compose up -d` | 手动控制 |
| **Kubernetes** | `helm install envnexus deploy/k8s/helm/envnexus ...` | 生产 K8s 集群 |
| **本地开发** | `make build` + 运行二进制 | Go 服务不使用 Docker 的开发模式 |

默认端口：console-web `:3000`、platform-api `:8080`、session-gateway `:8081`、job-runner `:8082`、agent-core `:17700`（仅本地访问）。

---

## 仓库结构

```text
envnexus/
├── apps/
│   ├── console-web/        # Next.js 14 管理控制台
│   ├── agent-desktop/      # Electron 30 桌面端
│   └── agent-core/         # Go 本地执行内核
├── services/
│   ├── platform-api/       # Go 核心 API（Gin + GORM，DDD 架构）
│   ├── session-gateway/    # Go WebSocket 网关
│   └── job-runner/         # Go 后台任务服务
├── libs/shared/            # Go 共享库
├── deploy/
│   ├── docker/             # Docker Compose 部署
│   └── k8s/helm/envnexus/ # Kubernetes Helm Chart
├── scripts/                # 冒烟测试、初始化数据
├── docs/                   # 用户手册、产品白皮书
└── Makefile
```

---

## 项目状态

所有核心模块功能完备（Phase 0–6 已实现）：

| 模块 | 状态 | 亮点 |
|------|------|------|
| Platform API | 已完成 | 认证、RBAC、Webhook、用量指标、License、飞书集成 |
| Session Gateway | 已完成 | WS 中继、事件去重、Redis pub/sub 水平扩展 |
| Job Runner | 已完成 | 7 个 Worker、原子任务抢占、审计归档 |
| Agent Core | 已完成 | 7 家 LLM、33+ 工具、执行计划生成、诊断引擎、治理引擎、OTA 更新 |
| Console Web | 已完成 | 12+ 页面、i18n（中/英）、统一 API 客户端 |
| Agent Desktop | 已完成 | 托盘、对话窗口、审批管理、自动更新 |
| 飞书集成 | 已完成 | 对话式执行/诊断、实时推送、审批卡片 |
| 基础设施 | 已完成 | 25 张表、Helm Chart、Docker Compose |

### 已知不足

- **测试**：集成/E2E 测试覆盖不足；无 OpenAPI 规范；无性能基准
- **企业级**：无 SSO（LDAP/SAML/OIDC）；无计费/计量；LLM 提示词仅支持英文
- **可观测性**：无分布式追踪（OpenTelemetry）；无 Prometheus/Grafana；数据库轮询任务队列
- **生态**：仅支持飞书集成（无 Slack/钉钉/企业微信/Teams）；不支持运行时插件加载

---

## 文档

- **用户操作手册**：[`docs/user-manual.md`](docs/user-manual.md) — 终端用户和管理员操作指南
- **商业产品白皮书**：[`docs/product-manual.md`](docs/product-manual.md) — 产品定位与商业方案

## 许可证

参见 [`LICENSE`](LICENSE)。
