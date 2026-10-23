# LumiFlow 前端

## 统一启动

```bash
cd /home/xin/work/LumiFlow
make up
```

页面默认访问 `http://127.0.0.1:8080`。根目录 Compose 会把前端和全部后端放入同一网络，只有该页面端口绑定到宿主机；Next.js BFF 通过 `http://live-api:8080` 访问内部网关。

常用命令：

```bash
make ports
make logs
make down
make check
```

`LUMIFLOW_API_ORIGIN` 只被 Next 服务端的 BFF 路由使用，不会暴露给浏览器。`.env.example` 仅用于脱离融合 Compose 的本地 `npm run dev` 调试；正常运行新系统不需要复制它。
