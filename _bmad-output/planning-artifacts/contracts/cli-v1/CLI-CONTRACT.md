---
title: 平台无关 fixture v1 CLI 契约
status: local-scope-complete
created: 2026-10-09
updated: 2026-10-09
---

# 本地 CLI 契约

用户于 2026-10-09 将本轮交付收敛为平台无关、可本地验证的手工核心；TikTok profile 转交 Epic 2，Ubuntu/systemd 和 OpenBao 验收继续阻塞。本契约覆盖该范围的 23 个操作；授权、刷新与平台验证的命令细化不被标记完成。

`hub [--socket <本地路径>] [--interactive]` 从 stdin 读取一个 JSON 对象直至 EOF；输入为 `version=contract/v1`、`operation` 和 `payload`。秘密只来自 stdin，或显式 interactive 的 `/dev/tty` 隐藏读取；interactive 的 stdin 提供不含 value 的导入元数据。参数不提供秘密输入入口。提示只写 stderr，成功 stdout 是单 JSON、退出 0；失败严格仅 error/message、退出 1。写出后发生 I/O 错误不追加第二 JSON，不暗示服务回滚。

HTTP/1.1 POST `/v1/operations` 仅通过 Unix Socket。Content-Type application/json，每连接一请求，不重试、不重定向。请求最大 1 MiB；总 CLI deadline 15s，包括 stdin 和交互。重复/未知字段、字段大小写别名、null 非可空字段、尾随对象、非法 ID 都拒绝。ID 按区分大小写的原字符串，1–128 UTF-8 字节，无空白、控制字符、竖线；不 trim、不从名称生成；外部数字 ID 仍是字符串。name 至多 256 字节，无控制字符。静态 schema 的字符长度不代替运行时字节限制。

| 操作 | 输入重点 | 输出 | 权限 |
| --- | --- | --- | --- |
| app.register / app.list | app_id、fixture platform、provider_app_id / {} | operation/revision / apps+revision | manage |
| shop.register / shop.list | external_shop_id、fixture platform、provider_shop_id / {} | operation/revision / shops+revision | manage |
| brand.register / brand.list | brand_id / {} | operation/revision / brands+revision | manage |
| internal_shop.register / internal_shop.list | internal_shop_id / {} | operation/revision / internal_shops+revision | manage |
| shop.map_brand / shop.unmap_brand | external_shop_id、expected_revision；建立时带 brand_id | operation/revision/row_revision | manage |
| brand.map_internal_shop / brand.unmap_internal_shop | brand_id、expected_revision；建立时带 internal_shop_id | operation/revision/row_revision | manage |
| installation.create / installation.list | App+shop+App expected_revision / 可选 App/shop 过滤 | operation/revision / installations+revision | manage |
| credential.import | scope、App、shop（shop scope）、type、value、replace、expiry_known、expires_at、confirm；替换带 expected_generation | operation/revision/generation/row_revision | manage |
| credential.confirm / credential.restore | scope、App、type、shop（适用时）、expected_generation | operation/revision/generation/row_revision | manage |
| credential.mark_invalid | 同上，加 reason=rejected/revoked/invalid | operation/revision/generation/row_revision | manage |
| credential.disable | 明确停用当前目标，无 expected_generation | operation/revision/generation/row_revision | manage |
| credential.status | 可选 App/shop 过滤 | 非敏感 statuses+revision | manage |
| credential.query | by=brand/internal_shop/external_shop，对应 ID，可选 App/shop 选择 | identity/credentials/status 三组 | read |
| health | {} | revision、audit_degraded、fixture mode、延期门槛 | manage |
| audit.list | UTC from/to、operation/target/result/operation_id、offset/limit | events/total/offset/limit | manage |

应用核心通过可信 ProviderRegistry 注入平台规则；第二虚构 profile 的复用测试证明核心不依赖 fixture 类型组合。本地服务、CLI 和机器 schema 当前只公开 fixture：App client_secret + shop shop_token；refresh_token 仅管理用途，不进入业务结果。这不是 TikTok 或其他真实平台的字段契约。客户端不可自定义必需组合、UID、角色或目的来扩大秘密输出。

所有实体列表及 credential.status 接受 offset/limit，默认 0/100，显式 limit 必须为 1–1000；结果返回 total/offset/limit/revision。installation.list 和 credential.status 另支持 App/shop 过滤。响应独立最大 16 MiB。非法 UTF-8 或 surrogate 拒绝；共享 DTO 中当前操作禁止的字段，即使为空或零也拒绝。隐藏 TTY 输入关闭回显及 canonical 缓冲，最多 65536 字节；取消和退出恢复终端。缺 peer 身份直接拒绝，审计以 Linux 保留 UID 4294967295 和 actor_name=unidentified 表示无法识别，不能解释为真实用户。

过期 expires_at 使用 UTC RFC3339，以服务现在判断；未知期限 false/null，必须确认当前 generation。换值递增 generation 并清空旧确认/验证证据，状态变化仅推进 row_revision；映射变更立即影响新查询。停用后显式替换产生恢复资格，确认与映射修复不能解除停用；restore 复核资格、版本、可用性和完整映射。App 级恢复保守要求已有安装全部链完整。

先核对内核 UID/权限，然后请求/对象、选择、归属、必需项、停用、过期及解密。错误码由 contract/v1 固定；依赖故障是 internal_error。不得以密码、密文、后端响应或用户输入拼接错误。管理提交断连可从非敏感 list/status 的 revision/row_revision/generation 复核；无法证明本次结果时不得盲重放，审计不是唯一提交依据。

业务查询响应只允许 identity、status 和 status.app/shop 状态条目增加可选非敏感元数据。客户端完整读取，验证已知类型、组合、版本和期限后重建输出；未知元数据不透传，未知秘密/type/重复项或截断响应整体拒绝。其他成功结果不允许扩展未知字段。审计按入库顺序和 UTC 时间稳定分页，默认 limit=100，最大1000；生产的一年保留与采集尚未验收。

机器请求 schema 由 `scripts/contract-schema.py` 从 Go DTO 导出；重复字段/EOF/字节上限和领域关系由 Go 测试验证，JSON Schema 不能独立证明这些规则。已实现的公开编码以 `internal/contract/v1` 为权威。运行和延期详情见本轮交付记录。
