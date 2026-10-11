# Event Workflow Platform

[![CI](https://github.com/zengqi7/event-workflow-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/zengqi7/event-workflow-platform/actions/workflows/ci.yml)

面向订单与运维系统的多租户事件和可靠 Webhook 工作流平台。用于订单超时后通知客服、再通知履约系统等有先后依赖的自动化：上游可安全重试，规则绑定不可变工作流，执行过程可以追踪和审计。

## 已实现能力

- 数据库 API Key 绑定 tenant、subject、scope、到期与吊销；请求配额、任务配额和审计。
- tenant/source/event_id 接口幂等，内容冲突返回 409。业务去重另按业务键和分钟窗口处理；MySQL 是裁决事实源，Redis 为辅助缓存。
- 工作流 JSON 是唯一事实源；发布验证 DAG、版本不可变，run 保存快照与摘要。依赖边保存状态，所有父任务成功后原子解锁。
- 事件、run、task 和 Outbox 同事务。Worker 租约、fencing、SKIP LOCKED，统一锁顺序结算；崩溃消耗尝试预算。
- retry 保留投递键；人工 replay 经审计创建新 run、新代次、新投递键，仅支持可信 runtime 快照。
- 生产 Webhook 默认拒绝，要求 HTTPS 443、精确主机白名单及租户 Secret 引用；防 DNS rebinding、私网、重定向和代理。HMAC 签名带时间戳。
- 数据库 round-robin 调度、租户执行配额、Worker 并发、Prometheus 指标及可选 OTLP/HTTP 追踪。

## 快速启动

需要 Docker Compose v2。复制模板后设置随机数据库密码。Compose 是本机开发配置，显式允许本地无签名 Webhook；生产按安全文档重新配置。

```powershell
Copy-Item .env.example .env
docker compose up --build -d
docker compose ps
$keyOutput = docker compose run --rm --no-deps --entrypoint key api -tenant 1 -subject local-demo -scopes events:write,state:read -ttl 24h
$env:EVENT_API_TOKEN = ($keyOutput | Where-Object { $_ -like 'token=*' }) -replace '^token=', ''
.\scripts\smoke-test.ps1
```

API：http://127.0.0.1:8080，sink：http://127.0.0.1:18081，Worker 指标：http://127.0.0.1:9091/metrics。端口可通过环境变量调整。停止使用 `docker compose down`，保留数据库卷。

## API 与开发

业务接口要求 Bearer API Key；租户来自凭证，可省略 tenant 字段。

| Method | Path | Scope |
|---|---|---|
| POST | /api/v1/events、/api/v1/events/batch | events:write |
| GET | /api/v1/events、/api/v1/workflow-runs、/api/v1/workflow-tasks、/api/v1/outbox-events | state:read |
| POST | /api/v1/workflows | workflows:write |
| POST | /api/v1/workflow-runs/{id}/replay | runs:replay |

完整请求见 [OpenAPI](docs/openapi.json)，定义见 [workflow-versions](docs/workflow-versions.md)。Go 版本以 go.mod 为准；使用 database/sql，不依赖 Ent。测试：`go test ./...`、`go vet ./...`；设置独立测试库 INTEGRATION_MYSQL_DSN 和 INTEGRATION_REDIS_ADDR 后运行 `go test -race -tags=integration ./...`。跨平台入口见 [local-run](docs/local-run.md)。

## 边界与运维

投递为 at-least-once，接收成功但提交前崩溃仍可重投，接收方必须按 Idempotency-Key 幂等。DAG 只支持静态 Webhook 依赖，无动态条件、人工审批、定时器或任意脚本。没有 RBAC 管理 UI、SSO、自动 retention 或已验证的生产 SLO。

服务启动检查迁移，不自动执行 DDL。正式上线需要 TLS、Secret 管理、告警、备份恢复演练与容量验证。

- [架构](docs/architecture.md) / [认证](docs/authentication.md) / [DAG](docs/dag-scheduler.md)
- [Replay 与出站安全](docs/replay-and-webhook-security.md) / [指标、故障、Benchmark](docs/operations-and-benchmarks.md)
- [发布、升级、恢复与兼容政策](docs/release-and-operations.md)
