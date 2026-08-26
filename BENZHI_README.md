# BENZHI_README

这是一个 Go 后端服务，用于协调制造企业机器人单元从勘测、审批、停机安装、标定验收到投产、维护和退役的全生命周期业务。

## 环境要求

- Go 1.25，module 为 `github.com/11DingKing/robotcell-lifecycle-control`。
- 默认使用项目内置的 SQLite 驱动，无需外部数据库服务。

## 标准构建、运行和测试命令

进入容器后执行：

```bash
# 编译
cd '/app' && GOTOOLCHAIN=local go build ./...

# 启动
cd '/app' && GOTOOLCHAIN=local go run ./cmd/server

# 测试
cd '/app' && GOTOOLCHAIN=local go test ./...
```

服务默认监听 `:8080`，存活检查为 `/healthz`，就绪检查为 `/readyz`。如需持久化数据库，可设置 `ROBOTCELL_DB_PATH` 指向可写目录。

## Docker 构建和进入容器

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh robotcell-lifecycle-control-amd64 linux/amd64
./build_benzhi_docker.sh robotcell-lifecycle-control-arm64 linux/arm64
docker run -it robotcell-lifecycle-control-amd64:latest
docker run -it --platform linux/arm64 robotcell-lifecycle-control-arm64:latest
```
