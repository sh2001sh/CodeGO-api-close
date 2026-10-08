# 主站消息接口

`notifications.New(pool, log).Register(mux, authenticate)` 注册以下接口。认证函数签名为 `func(*http.Request) (int64, error)`，由控制服务提供会话认证。所有响应使用现有 `{success,message,data}` JSON 封装，禁止缓存。

## 查询

- `GET /api/notifications?category=all&unread=false&page=1&page_size=20`
- `GET /api/notifications/summary`

分类允许 `all`、`market`、`billing`、`review`、`rewards`、`system`；留空等同全部。页码为 1–100000，页大小为 1–100；`unread` 只接受 `true` 或 `false`。

列表的 `data` 为：

```json
{
  "items": [{
    "id": "123",
    "category": "market",
    "kind": "multiplier_changed",
    "title_key": "notifications.multiplierChanged",
    "body_key": "notifications.multiplierChangedBody",
    "data": {},
    "action_url": "/channel-market",
    "created_at": "2026-10-07T00:00:00Z",
    "read_at": null
  }],
  "unread_count": 1,
  "latest_id": "123",
  "page": 1,
  "page_size": 20,
  "total": 1
}
```

`total` 受筛选条件影响。`unread_count` 和 `latest_id` 始终属于当前用户的全部通知，不受分类、已读筛选或分页影响。列表使用同一个可重复读事务快照。摘要返回 `{unread_count,latest_id}`；没有通知时 `latest_id` 为字符串 `"0"`。ID、金额和倍率整数以十进制字符串返回，客户端不得转换成不精确的浮点数。

## 修改已读状态

`POST /api/notifications/{id}/read` 接受 `{ "read": true }` 或 `{ "read": false }`。空请求体兼容默认标为已读；提供请求体时必须包含布尔 `read`。重复提交同一个状态具有幂等性，不重复发送变化事件。其他用户的 ID 与不存在的 ID 均返回 404。

`POST /api/notifications/read-all` 必须提供：

```json
{ "through_id": "123", "category": "market" }
```

`through_id` 是客户端列表快照的 `latest_id`，必须为正的 int64 十进制字符串。仅将当前用户对应分类、ID 不超过此值的通知标为已读，保留快照之后产生的消息。分类可省略，或兼容通过查询参数传入；请求体与查询参数同时提供不同分类时返回 400。空列表的 `"0"` 不能用于批量修改，界面应禁用该操作。

请求体限制 4 KiB；未知字段、畸形 JSON、多份 JSON、错误类型或无效 ID 返回 400。变更接口拒绝跨站请求。原倍率、幸运奖励接口与消息中心的已读/未读状态双向同步，迁移保留原有状态。

## 实时变化

`GET /api/notifications/events` 使用 `codego_session` Cookie，拒绝仅凭 Bearer 或 URL 参数建立连接；初次认证和后续会话复核都使用 Cookie。

- 首次返回 SSE `unread_count` 事件，数据为 `{unread_count,latest_id}`。
- 用户通知变化时返回 `invalidate`，数据格式相同；客户端更新摘要并使列表缓存失效。
- 每 20 秒发送注释心跳并复核会话，不定时查询消息或未读数。

同一进程的浏览器订阅共享一条 PostgreSQL `LISTEN` 连接，每用户最多 4 条 SSE 连接。事务提交才发布事件；批量变更合并为一次刷新。数据库监听断线后重新连接，并使在线用户的缓存失效；SSE 请求自己的写入、认证或查询失败仅终止该请求。写入与心跳都重设写入期限。控制服务应提供进程级 `BaseContext`，使停机取消长连接。

## 事件来源

| kind | 分类 | 数据字段 | 操作位置 |
|---|---|---|---|
| `multiplier_changed` | market | channel_id、previous_multiplier_ppm、multiplier_ppm、cleared | /channel-market |
| `bargain_requested` | market | request_id、group_id、channel_id、proposed_ppm | /my-channels |
| `bargain_resolved` | market | group_id、status、note、proposed_ppm | /channel-market |
| `shop_review` | review | shop_id、status、reason | /my-channels |
| `channel_review` | review | channel_id、field（name/source_label）、status、reason | /my-channels |
| `order_paid`、`order_refunded`、`order_failed` | billing | order_id、amount_minor、currency、state | /billing |
| `lucky_reward` | rewards | reward_id、final_reward_credits | /billing |

新议价通知发给渠道主，处理结果发给申请人；店铺与渠道审核结果仅发给所有者。审核备注只作为文本展示，不作为 HTML。通知与业务变更在同一个数据库事务中持久化，状态重复更新不制造重复通知。

## 验证

```powershell
go test ./internal/notifications ./migrations
go vet ./internal/notifications
$env:V3_NOTIFICATIONS_TEST_PG_DSN='postgres://postgres@127.0.0.1:55497/notifications_test?sslmode=disable'
go test -tags=pgintegration -count=1 ./internal/notifications
```

集成测试仅接受保留给本测试的 `notifications_test` 数据库，重建该数据库内的 v3 测试模式。不得将测试 DSN 指向其他数据库。
