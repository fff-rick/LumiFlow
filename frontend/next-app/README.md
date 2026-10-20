# LumiFlow 前端

## Docker 启动

```bash
cp .env.example .env
# 修改 .env 中的 LUMIFLOW_API_ORIGIN，使其指向 live-platform 网关
make up
```

页面默认访问 `http://127.0.0.1:3000`。常用命令：

```bash
make logs
make down
make check
```

容器运行时通过 `.env` 注入 `LUMIFLOW_API_ORIGIN`，该变量只被 Next 服务端的 BFF 路由使用，不会暴露给浏览器。Docker 启动时已经为 Linux Docker 映射 `host.docker.internal` 到宿主机；若网关同处一个 Docker 网络，请把地址换成对应服务名。
