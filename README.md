# Robot Cell Lifecycle Control

面向制造企业机器人单元全生命周期的生产级 Go 后端服务。系统把现场勘测、方案审批、停机窗口、安装标定、安全与质量验收、投产、维护和退役串联为可审计流程，并对工位、工装、人员资质和备件实施事务一致的并发占用控制。

## 核心能力

- 六类业务角色、密码登录、服务端会话、过期与退出撤销。
- 机器人单元、生产批次、作业窗口和维护工单的多步骤状态机。
- SQLite 关系数据库、版本化 migration、外键、约束、索引和乐观锁。
- 停机窗口的工位、工装与人员资质原子预留，重叠占用冲突裁决。
- 维护执行中的人员资质、备件预留与消耗跨实体事务。
- 标定失败恢复任务、租约认领、指数退避、永久失败和重启恢复。
- context 传播、统一错误结构、请求 ID、结构化日志、健康与就绪检查。
- 追加式哈希审计链，记录操作者、对象、动作、结果和关联请求。

## 本地运行

要求 Go 1.25 或更高兼容版本。默认数据库文件为当前目录的 `robotcell.db`，默认监听 `:8080`。

```bash
go mod download
go run ./cmd/server
```

首次启动会创建六个演示角色。密码由 `ROBOTCELL_BOOTSTRAP_PASSWORD` 指定，默认值仅适合本地体验；生产环境必须覆盖。

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
curl -X POST http://localhost:8080/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"line.manager","password":"change-this-password"}'
```

## 测试与质量检查

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
```

测试使用临时真实 SQLite 数据库，覆盖 migration、重启恢复、会话生命周期、权限、状态流、事务回滚、并发冲突、worker 重试与取消、HTTP 契约和审计链。

## Docker

```bash
docker build --platform linux/amd64 -t robotcell-lifecycle-control:amd64 .
docker run --rm -p 8080:8080 -v robotcell-data:/data robotcell-lifecycle-control:amd64
```

容器默认入口启动服务，并配置 `/healthz` 健康检查。可以把目标平台改为 `linux/arm64` 构建另一架构镜像。
