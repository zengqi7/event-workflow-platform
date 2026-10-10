# Event Workflow Platform

[![CI](https://github.com/zengqi7/event-workflow-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/zengqi7/event-workflow-platform/actions/workflows/ci.yml)

面向企业内部订单、风控和运维系统的多租户事件接入与异步工作流平台。平台解决上游重复投递、同一业务事件重复触发、通知失败难追踪，以及业务写入与异步任务双写不一致的问题。

## 核心链路

```text
HTTP event -> transport idempotency -> business dedup -> rule evaluation
           -> workflow run/task + transactional outbox
           -> leased worker -> webhook -> atomic status settlement
```

- **多租户接入**：事件、规则、工作流、执行记录和查询均带租户边界。
- **两层防重**：`tenant + source + event_id` 保证接口幂等，相同键但不同内容返回 `409`；`tenant + event_type + business_key + minute bucket` 抑制业务重复。
- **并发一致性**：MySQL 唯一约束在事务内裁决并发去重，Redis 只作为幂等辅助，不作为提交事实。
- **规则与工作流**：按优先级匹配 JSON 条件，命中后生成 run/task。
- **Transactional Outbox**：事件、run/task 和 Outbox 在同一事务提交。
- **可靠异步执行**：Worker 使用租约、`SKIP LOCKED`、重试退避和 fencing token；任务、Outbox、run 状态在同一事务结算。
- **Webhook**：传递原始业务 payload，并携带稳定的 `Idempotency-Key`。投递语义是 at-least-once，接收方需按该键幂等。
- **可观测性**：`/healthz`、`/readyz`、Prometheus 文本指标和结构化基础日志。

## 快速启动

要求 Docker Compose v2。复制环境变量模板并替换密码：

```powershell
Copy-Item .env.example .env
docker compose up --build -d
docker compose ps
```

Compose 会启动 MySQL 8.4、Redis 7.4、迁移任务、API、Worker 和本地 Webhook sink。默认只绑定本机：

- API: `http://127.0.0.1:8080`
- Webhook sink: `http://127.0.0.1:18081`
- MySQL: `127.0.0.1:3306`
- Redis: `127.0.0.1:6379`

验证完整链路：

```powershell
.\scripts\smoke-test.ps1
Invoke-RestMethod http://127.0.0.1:8080/metrics
```

停止服务：

```powershell
docker compose down
```

只有明确需要清空数据时才附加 `-v`。

## API

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/v1/events` | 接入单个事件 |
| `POST` | `/api/v1/events/batch` | 批量接入，最多 100 条 |
| `GET` | `/api/v1/events?tenant_id=1` | 查询租户事件 |
| `GET` | `/api/v1/workflow-runs?tenant_id=1` | 查询 run |
| `GET` | `/api/v1/workflow-tasks?tenant_id=1` | 查询 task |
| `GET` | `/api/v1/outbox-events?tenant_id=1` | 查询 Outbox |
| `GET` | `/healthz`, `/readyz`, `/metrics` | 健康、依赖和指标 |

示例请求：

```json
{
  "tenant_code": "acme",
  "source": "order-service",
  "event_type": "order.timeout",
  "event_id": "evt-20261010-0001",
  "business_key": "order-10001",
  "occurred_at": "2026-10-10T10:30:00Z",
  "payload": {
    "order_id": "10001",
    "amount": 6999
  }
}
```

初始迁移包含 `acme` 演示租户、高价值超时规则和 Webhook 工作流。

## 本地开发

```powershell
.\scripts\test.ps1
$env:MYSQL_DSN='event_app:<MYSQL_PASSWORD>@tcp(127.0.0.1:<MYSQL_PORT>)/event_workflow?parseTime=true&charset=utf8mb4&loc=UTC'
$env:REDIS_ADDR='127.0.0.1:6379'
go run ./cmd/migrate
go run ./cmd/http-server
go run ./cmd/worker
```

环境变量见 [.env.example](.env.example)，架构和可靠性决策见 [docs/architecture.md](docs/architecture.md)。

## 一致性边界

- 数据库唯一约束是幂等与去重的最终事实来源。
- Worker 崩溃后，超时租约可被其他实例接管；旧实例不能用过期 token 结算。
- Webhook 成功但数据库提交前进程崩溃会导致重投，这是 at-least-once 的正常边界。
- V1 面向可信内网，尚未提供 API Key/RBAC 管理面；不要将 API 直接暴露到公网。生产接入应置于认证网关之后。
- 工作流节点当前按扁平任务集合执行，不支持 DAG 条件分支；复杂分支属于后续版本。

## 测试

- 单元测试：请求校验、规则匹配、幂等/去重命令、HTTP、Worker 成功与重试。
- 集成测试：真实 MySQL/Redis 下验证幂等、并发去重、Outbox 到 Webhook 及三类状态一致性。
- GitHub Actions：`go test -race`、`go vet`、集成测试和容器构建。
