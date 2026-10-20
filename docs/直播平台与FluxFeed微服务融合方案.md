# LUMI LIVE 与 FluxFeed 微服务融合方案

**状态：方案评审稿**
**范围：将 FluxFeed 的视频内容域接入 LUMI LIVE；不合并两个仓库的业务库，不改变直播互动域的所有权。**

## 1. 结论

以 **LUMI LIVE 为统一入口和直播互动平台**，以 **FluxFeed 为 LUMI 的视频内容微服务及唯一登录服务**。FluxFeed 接管账号注册/登录、短视频发布、审核、详情、作者作品、Feed、视频互动和推荐；LUMI LIVE 保留 FluxFeed 用户的本地身份投影，用于直播间、实时互动、钱包礼物及实时推送。

不采用“把 FluxFeed 表复制到 `live-platform` 的 `video` schema”或“两个服务直连对方数据库”的方式。现有 `live-video` 仅有元数据 CRUD 与统计占位能力，不能替代 FluxFeed 已实现的审核、Feed、关系、互动、推荐和 Outbox；它应被 FluxFeed 兼容入口逐步替换并最终下线。

```text
Browser / LUMI Web（live-platform 基座）
        |
        v
live-gateway（唯一公网 API、身份校验、限流、Trace）
  |                         |
  | /api/v1/rooms/*         | /api/v1/videos/*、/api/v1/feed/*
  v                         v
LUMI LIVE 领域服务        FluxFeed Video Service
身份/房间/互动/交易          视频/审核/Feed/推荐/视频互动
  |                         |
  +------ Kafka：版本化集成事件 ------+
                 |
          各自数据库与 Redis
```

> 媒体字节继续放在对象存储/CDN；本次不是将短视频文件系统、推流、转码或 CDN 纳入直播互动服务。

## 2. 当前基线与问题

| 项目 | 已具备能力 | 融合时必须处理的差异 |
| --- | --- | --- |
| LUMI LIVE | `live-api` 网关、Identity/Room、Centrifugo、Kafka、礼物 Outbox；已有 `/api/v1/videos` 网关路由和 `live-video` | `live-video` 只保存视频元数据，使用 `sub` 型 HS256 JWT，视频接口为 `/api/v1/...` |
| FluxFeed | 视频审核状态机、视频/互动/关系/Feed/推荐、Kafka Outbox、内容治理 | API 为 `/api/...`，JWT 使用 `uid`/`role`/`token_type` 型 HS256 Token，拥有自己的账户模型与 Kafka 事件信封 |

因此不能直接把 `VIDEO_BASE_URL` 指向 FluxFeed：路径、响应语义和 JWT 声明均不兼容。也不能共用 `AUTH_JWT_SECRET`/`JWT_SECRET` 作为快速方案；对称密钥共享会让任一服务能够伪造另一服务的登录令牌。

## 3. 目标边界

### 3.1 服务所有权

| 域 | 唯一写入者 | 读写规则 |
| --- | --- | --- |
| 登录账号、账号状态 | FluxFeed | LUMI LIVE 不再签发浏览器登录令牌；网关用 FluxFeed access token 查询用户资料并投影为直播域用户 |
| 直播房间、禁言封禁、在线状态、弹幕、点赞、钱包、礼物 | LUMI LIVE | FluxFeed 不读写 LUMI LIVE 的 MySQL/Redis；LUMI 的身份投影只服务直播域 |
| 视频元数据、审核、视频互动、关注关系、Feed、推荐、内容举报 | FluxFeed | LUMI LIVE 不再写 `video.videos`/`video_stats`，也不复制为可写主表 |
| 视频文件、封面、播放清单 | 对象存储/CDN | 两个业务服务只保存受签名或公共 HTTP(S) URL；上传与转码单独演进 |
| 聚合展示（如直播间作品栏） | 可丢弃的投影 | 只读、可重建、允许最终一致，不能成为视频真相源 |

### 3.2 身份与用户映射

FluxFeed `user_id` 是登录和视频域主体。网关在首次登录或首次使用受保护直播接口时，将 FluxFeed 用户以 `external_identities(provider=fluxfeed, subject=fluxfeed_user_id)` 幂等投影到 LUMI Identity；LUMI 的 `user_id` 只用于直播域内部主键。

项目尚无线上数据，不执行历史用户或视频导入，也不维护双写或 `legacy_video_links`。跨域关联一律保存两个服务各自返回的 ID；禁止用邮箱作为跨服务主键。

## 4. 接入接口与鉴权

### 4.1 网关路由

公网仍只暴露 `live-gateway`。FluxFeed 已有视频 API 与 LUMI v1 路径仅差 `/v1` 前缀，网关按以下白名单直连其原生 Handler；不再增加 Facade 或复制视频业务逻辑。

| LUMI LIVE 外部接口 | FluxFeed 原生 Handler | 说明 |
| --- | --- | --- |
| `POST /api/v1/videos` | `POST /api/videos` | 保留 `Idempotency-Key`；响应返回 FluxFeed 审核状态，不假称一定已发布 |
| `POST /api/v1/content-assets` | `POST /api/uploads` | 受保护的 multipart 上传；FluxFeed 校验并记录资产所有权 |
| `GET/DELETE /api/v1/videos/{id}` | 获取/软删除视频 | 仅作者可删；公开读取只返回已发布内容 |
| `GET /api/v1/users/me/videos`、`/users/{id}/videos` | 作者作品列表 | 保持 FluxFeed 的 `limit`/`offset` 分页 |
| `GET /api/v1/feed-items` | FluxFeed Feed | 不塞入直播间查询服务 |
| `PUT/DELETE /api/v1/videos/{id}/like` | FluxFeed 视频点赞 | 与直播间点赞严格区分资源和指标 |

`live-gateway` 按路由精确转发到 FluxFeed；不要用通配反向代理把 FluxFeed 的 `/api` 全部暴露到公网。无历史数据时，视频路由可一次性切换；旧 `live-video` 只保留本地开发回退，待联合部署稳定后删除。

FluxFeed 发布会进入审核并可能返回 `202 Accepted`；这是融合后的正式语义。客户端直接以 FluxFeed 的审核状态展示内容，网关不伪造旧 `live-video` 的“立即发布”结果。

### 4.2 鉴权方案

浏览器只持有 FluxFeed access token。网关将该 token 原样转发给 FluxFeed 视频路由；进入 LUMI 直播域时，网关在线校验 FluxFeed 用户资料并换发仅供 LUMI 内部服务使用的短时 JWT。浏览器和其他容器不可直达 FluxFeed 管理端口。

直播域内部 JWT 只由网关签发和验证，使用独立于 FluxFeed 用户 JWT 的密钥；FluxFeed 不接受这个内部 JWT。后续可将网关对 FluxFeed access token 的在线校验改为 FluxFeed 发布公钥的本地验证，以降低身份查询开销。

LUMI 内部服务从网关换发的 JWT 取得用户，不接受客户端传入的 `X-User-ID`。FluxFeed 原有登录/OAuth 路由正是融合后的唯一登录源，不再保留第二套 LUMI 浏览器登录。

#### 第一阶段实现：FluxFeed 在线校验桥接

登录改造已先按更小的可回滚闭环实现：`live-gateway` 将 `/api/v1/auth/register` 和 `/api/v1/auth/login` 转发为 FluxFeed 的 `/api/users`、`/api/sessions`；Next BFF 将 FluxFeed access token 写入 HttpOnly Cookie，前端脚本不可读取。每个受保护的直播请求由网关调用 FluxFeed `/api/users/me` 校验，并将用户资料同步为 Identity 的只读投影，再向直播内部服务替换为短时 LUMI token。内部服务不接受 FluxFeed token，避免在每个服务散布跨系统鉴权逻辑。

该在线校验在网关按 token 哈希缓存 30 秒，用于限制互动高频请求对身份服务的压力；禁用账户的最长生效延迟为 30 秒。部署时设置 `FLUXFEED_BASE_URL`、`INTERNAL_SERVICE_TOKEN`，并保持 LUMI 的 `AUTH_JWT_SECRET` 仅用于网关到直播内部服务。下一阶段将以 FluxFeed 的非对称签名验证密钥替换在线校验缓存。

## 5. 数据与事件契约

### 5.1 数据原则

当前没有正式线上用户和视频数据，因此不做导入、ID 映射、双写或旧链接兼容。新产生的账号和视频只写 FluxFeed；`live-video` 数据库不再是任何视频功能的来源。

### 5.2 集成事件

保留两项目当前 Topic 和信封，不互相消费：LUMI LIVE 使用 `event_id/event_type/room_id`，FluxFeed 使用 `id/type/user_id/video_id`。新增边界专用 Topic 和独立 JSON Schema：

| Topic | 事件 | 生产者 → 消费者 | 分区键 |
| --- | --- | --- | --- |
| `integration.video.v1` | `video.published`、`video.visibility_changed`、`video.deleted` | FluxFeed → LUMI LIVE 视频投影/Web UI | `flux_video_id` |
| `integration.room.v1` | `room.started`、`room.stopped`、`room.owner_changed` | LUMI LIVE → FluxFeed Feed 投影 | `room_id` |
| `integration.identity.v1` | `user.disabled`、`user.profile_changed` | FluxFeed → LUMI LIVE 直播域身份投影 | `fluxfeed_user_id` |

统一集成 envelope：

```json
{
  "event_id": "uuid", "event_type": "video.published", "version": 1,
  "occurred_at": "2026-10-19T08:00:00Z", "producer": "fluxfeed",
  "subject": {"fluxfeed_user_id": 42, "flux_video_id": 9001},
  "payload": {}
}
```

所有生产都通过各自 Transactional Outbox；消费为至少一次语义，以 `(consumer_name,event_id)` 去重。不可解析或违反 Schema 的消息进入对应 `.dlq`，成功写入 DLQ 前不得提交 offset。Trace context 通过 Kafka headers 传递，不写入业务 payload。

## 6. 部署、网络与可观测性

- 创建根目录集成 Compose/Kubernetes overlay，使用**一个** Kafka 集群；FluxFeed 指向 LUMI 的 `kafka:19092`，移除第二套 Kafka 和 RabbitMQ 兼容链路的生产依赖。
- MySQL 与 Redis 仍按服务隔离：不同数据库账号、PVC、Redis logical DB/实例和 key 前缀。不得复用当前两个项目都可能占用的宿主机 `6379`、`8080`、`9092`。
- 网关只公开 `443`；FluxFeed API、worker、数据库和 Kafka 仅在私有网络。内部调用启用 mTLS、NetworkPolicy/安全组与最小化 Secret 挂载。
- 使用同一 OpenTelemetry Collector、Tempo、Prometheus 和 Grafana；统一资源属性 `service.name`（`live-gateway`、`fluxfeed-api`、`fluxfeed-worker`）、`deployment.environment` 和 `trace_id` 日志字段。
- 最低监控：FluxFeed 的鉴权失败/限流/5xx/P95、视频审核积压、Outbox 待处理最老年龄、Kafka consumer lag/DLQ、Feed 缓存命中率、用户映射失败、事件投影滞后。

## 7. 分阶段实施与回滚

| 阶段 | 交付与切换条件 | 回滚方式 |
| --- | --- | --- |
| P0：登录与契约冻结 | FluxFeed 注册/登录为唯一入口；网关完成直播域身份投影；补充路由契约测试 | 关闭 `FLUXFEED_BASE_URL`，仅用于本地回退 |
| P1：视频 API 切换 | 视频发布、详情、删除和作品列表全部转到 FluxFeed；验证审核、幂等和 Feed | 停止视频写入并返回维护错误；不写回旧库 |
| P2：运行底座 | 部署共享 Kafka、隔离网络、统一观测和端到端冒烟 | 关闭视频路由，不影响直播互动 |
| P3：清理旧实现 | 联合环境稳定、备份恢复通过后，下线 `cmd/video`、`video` schema 和旧迁移 | 不写回旧库；视频域保持维护模式直到 FluxFeed 恢复 |

由于没有历史数据，视频 API 在联合环境验证后可一次性切换；开关仅保留在网关，避免客户端和多个后端各自判定流量。

本地 P2 联调使用 `cd live-platform && docker compose -f docker-compose.yml -f docker-compose.integration.yml -p lumiflow up --build -d live-api prometheus` 启动。该覆盖文件为 FluxFeed 创建独立 MySQL/Redis，复用 LUMI 的 Kafka、OTel Collector 和 Prometheus；`live-api` 会连带启动 FluxFeed worker。除网关外，FluxFeed API、worker、数据库、Kafka 和观测组件均不映射宿主机端口；FluxFeed API 仅由 `live-api` 经 `http://fluxfeed-api:8080` 访问。P2 默认在宿主机 `18080` 暴露网关，避免与单独启动的 LUMI 环境冲突；服务就绪后执行 `./scripts/p2_fluxfeed_smoke.sh`，验证注册、直播域身份投影、受控资产上传、视频创建/幂等重试/列表/删除及 Feed 路由。上传卷仅用于本地联调；生产部署必须改接对象存储/CDN。

## 8. 验收门槛

1. 同一 `Idempotency-Key` 重试只产生一条 FluxFeed 视频和一条发布事件；网关超时后的重试结果一致。
2. 已禁用的 FluxFeed 用户在 1 分钟内不能继续发布、互动或获得个性化 Feed；无效 access token 被拒绝且不泄露用户信息。
3. FluxFeed/Kafka/审核故障只让视频域降级为明确错误或缓存读，不影响开播、进房、弹幕、礼物和 Centrifugo 订阅。
4. 视频删除/下架事件在约定投影时延内从作品列表和 Feed 消失；重复、乱序事件不造成数据复活。
5. 压测分别验证直播热点房间与 Feed 高并发请求；两套 SLO、容量指标和告警互不掩盖。
6. 完成联合部署、备份恢复和 P1/P2 的回滚演练后，才允许下线旧 `live-video`。

## 9. 首批变更清单

1. 在 `live-platform`：网关以 FluxFeed 为登录和视频路由唯一来源；定义 `integration.*.v1` 契约。
2. 在 FluxFeed：保持原生账号、视频、审核、Feed 和 Outbox 实现，补充 LUMI 集成事件 producer。
3. 在根目录：增加集成环境部署清单、Secret 模板、Topic 初始化、Dashboard 和端到端冒烟脚本。
4. 先完成 P0 路由契约和 P1 视频链路测试，再建设运行底座；不做数据导入。

## 10. 本方案明确不做的事

- 不把 FluxFeed 的推荐、审核或视频互动塞入 LUMI LIVE 的 `live-query`/`live-interaction` 服务。
- 不合并 MySQL、Redis、JWT 对称密钥或用户表；不让任一服务直连另一服务数据库。
- 不在本次融合中建设直播推流、录制、转码、DRM、多地域 CDN 或支付结算。
