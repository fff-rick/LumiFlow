# LumiFlow 内部 gRPC 联调与性能优化方案

**状态：设计稿，尚未实施**

**日期：2026-10-22**

**范围：仅设计系统内部同步调用的 gRPC 演进方案，不改变浏览器协议、公开 HTTP API、数据库所有权或 Kafka 事件边界。**

## 1. 目标与结论

LumiFlow 对外继续提供 HTTP/JSON，Next.js 前端无须感知内部协议变化。gRPC 只用于容器或 Pod 之间高频、轻量的同步调用，首期聚焦两个收益明确的链路：

1. `live-interaction`、`live-commerce` 调用 `live-identity-room` 查询房间状态和用户权限。
2. `live-gateway` 调用 FluxFeed 校验 access token。

不建议一次性把全部内部 HTTP 改写成 gRPC。当前视频上传、媒体 Range、OAuth 回调、Centrifugo 和 Kafka 都不适合因“协议统一”而改造。是否继续扩大 gRPC 范围，应由首期压测结果决定。

## 2. 当前调用基线

当前内部同步调用主要使用 HTTP：

| 调用方 | 被调用方 | 当前方式 | 主要用途 |
| --- | --- | --- | --- |
| `live-gateway` | FluxFeed API | HTTP | 登录、注册、OAuth、token 校验、视频和 Feed 代理 |
| `live-gateway` | LUMI 领域服务 | HTTP 反向代理 | 房间、互动、交易和查询 |
| `live-interaction` | `live-identity-room` | HTTP，800ms 超时 | 房间状态、封禁、禁言和用户昵称 |
| `live-commerce` | `live-identity-room` | HTTP，800ms 超时 | 房间状态、房主和封禁状态 |
| LUMI Worker | `live-identity-room` | HTTP | 同步 FluxFeed 用户投影 |
| LUMI 服务 | Centrifugo | HTTP | 实时消息发布和健康检查 |

真实实现位置：

- Gateway 的 FluxFeed 认证桥接：[`../live-platform/internal/gateway/fluxfeed_auth.go`](../live-platform/internal/gateway/fluxfeed_auth.go)
- Gateway 的内部 HTTP 代理：[`../live-platform/internal/gateway/server.go`](../live-platform/internal/gateway/server.go)
- Identity/Room HTTP 客户端：[`../live-platform/internal/identityclient/client.go`](../live-platform/internal/identityclient/client.go)
- Interaction 装配：[`../live-platform/cmd/interaction/main.go`](../live-platform/cmd/interaction/main.go)
- Commerce 装配：[`../live-platform/cmd/commerce/main.go`](../live-platform/cmd/commerce/main.go)

当前高频链路存在重复网络往返：

- 弹幕需要查询房间访问权限，再查询用户昵称。
- 礼物需要分别查询房间状态和封禁状态。
- 点赞缓存未命中时需要分别查询房间状态和封禁状态。

因此，首期优化不能只是把两个 HTTP 请求机械替换成两个 gRPC 请求，还应提供一次聚合权限查询。

## 3. 目标架构

```text
Browser / Next.js
        │ HTTP/JSON
        ▼
LUMI Gateway
   ├─ HTTP ──> FluxFeed 登录、OAuth、视频上传、媒体访问
   ├─ gRPC ──> FluxFeed Token 校验
   ├─ gRPC ──> Identity/Room
   ├─ gRPC ──> Interaction
   ├─ gRPC ──> Commerce
   └─ gRPC ──> Query

Interaction / Commerce
        └─ gRPC ──> Identity/Room 权限与房间状态
```

边界原则：

- 浏览器、Next.js 和公开 API 继续使用 HTTP/JSON。
- Gateway 对外路径、状态码和 JSON 错误结构保持兼容。
- 服务仍然只读写自己拥有的数据库，不因 gRPC 共享数据库。
- Kafka 继续承担异步事件，gRPC 不替代 Transactional Outbox。
- 客户端不能直接访问内部 gRPC 端口。

## 4. 改造范围与优先级

| 优先级 | 链路 | 方案 | 预期收益 |
| --- | --- | --- | --- |
| P0 | Interaction/Commerce → IdentityRoom | gRPC 聚合权限查询 | 高频链路从多次调用降为一次 |
| P0 | Gateway → FluxFeed token 校验 | 增加内部 gRPC 身份服务 | 减少 JSON 和连接管理开销 |
| P1 | Gateway → LUMI 领域服务 | 按服务逐步引入 gRPC | HTTP/2 多路复用、统一超时和错误码 |
| 保留 HTTP | 登录、注册、OAuth | 不改 | 频率低，涉及浏览器跳转和 Cookie |
| 保留 HTTP | 视频上传、封面和视频访问 | 不改 | multipart、流式响应和 Range 更适合 HTTP |
| 保留现状 | Centrifugo、Kafka | 不改 | 不属于同步业务 RPC |

## 5. 协议组织

建议建立独立、可做兼容性检查的协议目录：

```text
proto/
├── fluxfeed/identity/v1/identity.proto
└── lumiflow/identityroom/v1/identity_room.proto
```

约束：

- Protobuf package 从 `v1` 开始，破坏性变更必须发布新版本。
- 字段编号一旦发布不得复用。
- 时间统一使用 `google.protobuf.Timestamp`。
- 业务主键继续使用 `int64`，不在边界隐式转换两个系统的用户 ID。
- CI 使用 Buf 或等价工具检查格式和 breaking change。
- 生成代码由固定版本工具产生，不手工编辑。

## 6. 核心服务契约

### 6.1 FluxFeed 身份校验

```protobuf
service FluxIdentityService {
  rpc GetCurrentUser(GetCurrentUserRequest)
      returns (PlatformUser);
}

message GetCurrentUserRequest {}

message PlatformUser {
  int64 user_id = 1;
  string account = 2;
  string nickname = 3;
  string avatar_url = 4;
  bool enabled = 5;
}
```

FluxFeed access token 通过 gRPC metadata 传递：

```text
authorization: Bearer <fluxfeed-access-token>
```

Gateway 获得用户资料后，继续使用现有映射逻辑创建或刷新 LUMI 本地用户，并签发短时 LUMI 内部 JWT。现有30秒 token 缓存保留，并建议加入 `singleflight`，避免同一个 token 在缓存失效时产生并发回源。

登录、注册和 OAuth 仍走 HTTP；这些接口不是当前性能热点。

### 6.2 LUMI 房间操作上下文

```protobuf
service IdentityRoomInternalService {
  rpc GetRoomActionContext(RoomActionContextRequest)
      returns (RoomActionContext);

  rpc SyncExternalUser(SyncExternalUserRequest)
      returns (LocalUser);

  rpc GetRoom(GetRoomRequest)
      returns (Room);

  rpc JoinRoom(JoinRoomRequest)
      returns (Room);
}

message RoomActionContextRequest {
  int64 room_id = 1;
  int64 user_id = 2;
}

message RoomActionContext {
  int64 room_id = 1;
  int64 anchor_id = 2;
  string room_status = 3;
  bool banned = 4;
  bool muted = 5;
  string nickname = 6;
}
```

`GetRoomActionContext` 一次返回房间状态、房主、封禁、禁言和用户展示名，可满足：

- 弹幕：检查直播状态、封禁、禁言并取得昵称。
- 点赞：检查直播状态和封禁。
- 礼物：检查直播状态、封禁并取得房主 ID。

预期调用次数变化：

| 操作 | 当前 | 目标 |
| --- | ---: | ---: |
| 发送弹幕 | 2次内部 HTTP | 1次 gRPC |
| 赠送礼物 | 2次内部 HTTP | 1次 gRPC |
| 点赞缓存未命中 | 2次内部 HTTP | 1次 gRPC |

## 7. Gateway 兼容层

Gateway 继续接受现有 HTTP 请求，将请求转换成内部 Protobuf，再把 gRPC 响应转换成现有 JSON。前端不需要改动。

错误映射建议：

| gRPC Code | HTTP | 使用场景 |
| --- | ---: | --- |
| `INVALID_ARGUMENT` | 400 | 参数错误 |
| `UNAUTHENTICATED` | 401 | token 无效 |
| `PERMISSION_DENIED` | 403 | 封禁或权限不足 |
| `NOT_FOUND` | 404 | 房间或用户不存在 |
| `ALREADY_EXISTS` | 409 | 资源冲突 |
| `FAILED_PRECONDITION` | 409 | 房间状态不允许操作 |
| `RESOURCE_EXHAUSTED` | 429 | 限流 |
| `UNAVAILABLE` | 503 | 服务不可用 |
| `DEADLINE_EXCEEDED` | 504 | 调用超时 |

业务错误通过 `google.rpc.ErrorInfo` 携带稳定原因码，例如 `ROOM_NOT_LIVING`、`USER_BANNED`、`USER_MUTED`。Gateway 负责转换成当前公开 API 的错误结构。

## 8. 连接、超时与重试

每个进程为每个上游维护一个长期 `grpc.ClientConn`，不能按请求创建连接。连接使用 HTTP/2 多路复用，并通过 Docker DNS 或 Kubernetes Service 做服务发现。

建议初始值：

| RPC | Deadline | 自动重试 |
| --- | ---: | --- |
| FluxFeed token 校验 | 800ms | 最多1次 |
| 房间操作上下文 | 300ms | 最多1次 |
| 用户映射同步 | 500ms | 不自动重试 |
| 普通只读查询 | 800ms | 最多1次 |
| 写操作 | 1.5s | 默认不重试 |

重试规则：

- 只自动重试只读 RPC。
- 仅对 `UNAVAILABLE` 等瞬时故障重试，不重试业务错误。
- 写操作只有携带幂等键且服务端完成去重时才能重试。
- 重试必须服从原请求总 deadline，不能叠加成无界等待。
- 初始采用小幅指数退避并添加 jitter。

## 9. 安全设计

- 本地 Docker 可以使用内网 h2c，生产环境使用 mTLS。
- 用户身份继续放在 `authorization` metadata 中。
- 迁移期间可将现有内部令牌放在 `x-internal-token` metadata；稳定后优先使用 mTLS 服务身份替代共享令牌。
- Gateway 不接受客户端提供的 `user_id` metadata，用户 ID 必须来自已验证 token。
- 日志、Trace、错误信息和指标不得记录完整 access token。
- gRPC reflection 只在开发环境开放。
- 限制最大消息大小；大文件和媒体流不经过这些 RPC。

## 10. 可观测性

客户端和服务端统一安装 gRPC 拦截器：

- 传播 OpenTelemetry trace context。
- 记录 RPC 请求量、耗时直方图和状态码。
- 记录当前并发、deadline exceeded、retry 和 unavailable 次数。
- 记录请求/响应大小分布。
- 日志包含 `service.name`、RPC service/method、trace ID 和耗时。

禁止把 `user_id`、`room_id`、token 或错误正文作为 Prometheus 标签，避免高基数和敏感数据泄露。

健康检查使用标准 `grpc.health.v1.Health`。进程存活与业务就绪分开：只有数据库、Redis 等必要依赖可用时才返回 `SERVING`。

## 11. 部署约定

建议双端口运行，迁移期间保留 HTTP：

| 服务 | HTTP | gRPC 建议端口 |
| --- | ---: | ---: |
| FluxFeed API | 8080 | 9080 |
| `live-commerce` | 8081 | 9081 |
| `live-interaction` | 8082 | 9082 |
| `live-identity-room` | 8083 | 9083 |
| `live-query` | 8084 | 9084 |

Docker 只 `expose` gRPC 端口，不映射到宿主机。Kubernetes Service 使用命名端口 `http`、`grpc`，NetworkPolicy 只允许明确的调用方访问 `grpc`。

建议迁移配置：

```text
INTERNAL_RPC_PROTOCOL=http|grpc
IDENTITY_ROOM_GRPC_TARGET=live-identity-room:9083
FLUXFEED_IDENTITY_GRPC_TARGET=fluxfeed-api:9080
```

协议开关用于迁移和回滚，不作为长期按请求双写开关。

## 12. 分阶段实施

### 阶段 G0：基线

1. 固定现有 HTTP 的功能测试和性能基线。
2. 分别记录 token 校验、弹幕、点赞、礼物的内部调用次数、P50/P95/P99、CPU 和失败率。
3. 明确现有错误码、超时和降级语义。

### 阶段 G1：协议和双栈服务端

1. 建立 `.proto` 和兼容性检查。
2. IdentityRoom 增加 gRPC 端口，复用现有 Service/Repository，不复制业务逻辑。
3. FluxFeed 增加内部身份校验 gRPC，继续由原账户服务验证 token。
4. HTTP 接口保持运行。

### 阶段 G2：只读影子验证

1. 客户端仍以 HTTP 结果为准。
2. 对少量只读请求异步调用 gRPC，对比响应内容。
3. 影子调用不得用于写命令，不得影响主请求延迟。
4. 差异率归零后进入切流。

### 阶段 G3：高频链路切流

1. Interaction 先切换 `GetRoomActionContext`。
2. Commerce 随后切换。
3. Gateway 最后切换 FluxFeed token 校验。
4. 按实例或调用方灰度，不同时切换全部链路。

### 阶段 G4：评估后续范围

只有首期达到性能门槛，才考虑 Gateway 到 IdentityRoom、Interaction、Commerce、Query 的其他调用。登录、OAuth、媒体上传和视频访问继续使用 HTTP。

### 阶段 G5：收敛

稳定观察至少一个发布周期后，才能移除已被完全替代的内部 HTTP 接口。公开 HTTP API 永久保留。

## 13. 回滚方案

- 服务端在迁移期同时提供 HTTP 与 gRPC。
- 客户端通过 `INTERNAL_RPC_PROTOCOL` 选择协议。
- 回滚只切换配置并重启调用方，不修改数据。
- 不允许对写操作同时发送 HTTP 和 gRPC，防止重复订单、重复点赞或重复状态变更。
- gRPC 故障时是否自动回退 HTTP 必须按调用类型决定：只读查询可以受控回退，写请求禁止无幂等保证的自动回退。

## 14. 性能验收标准

在相同硬件、数据量、请求模型和业务成功率下比较 HTTP 基线与 gRPC：

1. 弹幕、点赞缓存未命中、礼物的 IdentityRoom 网络调用下降为一次。
2. 高频业务链路端到端 P95 至少下降15%。
3. 相同 RPS 下应用 CPU 使用率至少下降10%，或相同 CPU 下稳定吞吐提升20%。
4. gRPC 业务失败率不高于原 HTTP。
5. HTTP 与 gRPC 的业务结果和错误语义一致。
6. 切回 HTTP 不需要数据修复。
7. 视频上传、播放、OAuth、Kafka 和 Centrifugo 指标无回归。

如果未达到以上门槛，应保留 HTTP 并停止扩大 gRPC 改造范围。gRPC 本身不是性能保证，减少重复调用、连接复用和正确的缓存策略通常比协议替换更重要。

## 15. 本方案明确不做

- 不向浏览器暴露 gRPC 或引入 gRPC-Web。
- 不用 gRPC 替代 Kafka、Outbox 或 Centrifugo。
- 不通过 gRPC 共享或代理数据库访问。
- 不把视频文件、封面、multipart 上传或 Range 下载封装进普通 unary RPC。
- 不在首期重写所有 Gateway 代理接口。
- 不改变 FluxFeed 作为统一登录注册和视频微服务的系统边界。
