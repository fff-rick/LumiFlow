# LumiFlow 前端

## 统一启动

```bash
cd /home/xin/work/LumiFlow
make up
```

页面默认访问 `http://127.0.0.1:8080`。Next.js BFF 通过 `http://live-api:8080` 访问内部网关；SRS 另外公开 `1935/TCP` 给 OBS、`1985/TCP` 给 WHEP 信令、`8000/UDP` 给 WebRTC 媒体，并通过 `8085/TCP` 提供 MP4 回放。

常用命令：

```bash
make ports
make logs
make down
make check
```

`LUMIFLOW_API_ORIGIN` 只被 Next 服务端的 BFF 路由使用，不会暴露给浏览器。直播页在本机通过 `ws://127.0.0.1:8000/connection/websocket` 连接 Centrifugo，非本机部署使用当前站点同源 WebSocket 入口。`.env.example` 仅用于脱离融合 Compose 的本地 `npm run dev` 调试；正常运行新系统不需要复制它。

## OBS + WHEP 直播

1. 主播打开自己的直播间，点击「生成推流凭证」。
2. OBS 选择“自定义”服务，分别填入页面显示的服务器和推流码。
3. OBS 使用 H.264 视频编码并开始推流；页面媒体状态变为 `PUBLISHING` 后点击「开始直播」。
4. 观众打开同一直播间，浏览器会直接与 SRS 完成 WHEP 协商并播放。
5. 结束直播并在 OBS 停止推流后，SRS 会封装 MP4；直播页自动显示可切换的录像片段。

主播临时断流后可以直接在 OBS 中重新开始推流；每次断流会形成独立录像片段。确认不再重连时，点击直播间里的「结束直播」，再停止 OBS 推流。本地融合环境可运行 `make -C live-platform smoke-srs-media`，自动验证 RTMP 推流、SRS 状态回调、WHEP 协商、MP4 回放和停播闭环。

跨设备测试时，启动前先设置 `SRS_CANDIDATE=<宿主机局域网 IP>`，同时将 `MEDIA_RTMP_SERVER_URL`、`MEDIA_WHEP_BASE_URL` 和 `MEDIA_RECORDING_BASE_URL` 配成客户端可访问地址。HTTPS 页面必须通过 HTTPS 反向代理暴露 WHEP 与录像文件，避免浏览器拦截混合内容。
