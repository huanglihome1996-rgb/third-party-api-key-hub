---
title: Party 修订技术依据与适用性独立审查
reviewed: 2026-10-08
updated: 2026-10-08
verdict: pass
scope: PM-01 至 PM-05 的技术依据、可实现性与实施门槛
---

# 技术审查

本轮静态文档审查通过，未解决发现为零。初读发现的一项 P2 规则作用域冲突已在最终复读中关闭。未发现新规则将 OpenBao、Go 或 TikTok 未验证能力冒充已经实现；保留既有版本和 SQ 门槛。审查只读 spine、credential-state、response-compatibility、pending-results、technology-evidence、实施门槛及 Story 1.2／1.6／2.4／2.5／2.7，没有运行真实平台、KMS 或业务实现。

## PTRT-01 — P2：CAS 修订号禁止绑定规则与结果包快照绑定冲突

- **证据**：ARCHITECTURE-SPINE.md 的 Consistency Conventions「状态版本」写“用于 CAS 的修订号绝不写入密文绑定”；Story 1.2 首个 AC 同样写“row_revision 与 authorization_epoch 不加入密文绑定”。pending-results.md 的保护对象则明确要求结果包认证绑定包含 session／epoch 和完整已知依赖 row_revision 快照；Story 1.2 后续 AC 和 Story 1.6／2.4 沿用此要求。
- **影响**：当前凭据不能绑定可独立变化的状态修订号，这一规则正确；暂存结果恰须绑定领取时的版本快照。实现者若按总括禁止规则省略结果包快照绑定，将无法满足新加的跨快照篡改拒绝验收；若一律包含又会破坏当前凭据的独立状态更新规则。
- **修复**：在 spine 和 Story 1.2 的禁止规则中显式限定 purpose=current-credential；同时说明 purpose=pending-provider-result 绑定不可变的领取快照与会话 epoch，提交时再核对当前状态。不能以 companion 覆盖总括规则的隐含解释代替消除冲突。
- **验收**：当前密文在不换值的确认／停用／映射状态变化后仍可按原业务绑定解密；待处置包篡改 epoch 或任一依赖快照则拒绝，正确原快照可解密但当前条件改变时不得提交。
- **状态**：已关闭。最终复读 spine 第 140 行和 epics.md 第 274 行，禁止规则已明确限定 current-credential，pending-provider-result 则认证绑定领取时快照和适用 epoch，解密仍复核当前 CAS／epoch。与 pending-results.md 及后续 Story 验收一致。这里只关闭文档冲突，以上运行验收继续受 1.2／1.6 门槛约束。

## 五项核查结论

1. **用途与 operation 绑定：最终静态规则通过。** 上述作用域冲突已修复；pending-results 的 purpose／operation／App／epoch／profile／依赖快照字段、从暂存结果重新保护为当前凭据的过程本身可实现；不存在需要 Transit 理解领域 operation 的断言。实际包/schema/认证编码留给 1.2，交换实证留给 1.6。
2. **密钥轮换与旧密文：静态规则通过。** credential-state 明确轮换／重新加密不改变业务 generation；1.6 以保留旧密钥版本为前提验证旧密文，并验证缺失/错误版本安全拒绝。没有承诺删除旧版本后仍能解密，也未把密钥轮换当业务换值或恢复资格。
3. **远端结果与崩溃：静态规则通过。** 请求前持久化领取记录，结果完整保护与操作状态同事务；KMS/DB 故障及崩溃不能明文落盘或重放平台。数据库不可写时可能无法立即写 indeterminate，但既有未终结领取记录仍触发启动阻塞；不依赖故障之后必然能追加新记录。实际状态矩阵与业务恢复仍分别受 1.6 和 2.4／2.5／2.7 门槛约束。
4. **CLI JSON 兼容：静态规则通过。** 固定扩展位置、已知字段验证、未知元数据忽略并重建 stdout，未知秘密/重复字段拒绝均可在 Go 实现。普通 encoding/json 解码不会自动拒绝重复字段，实施必须按 1.1 契约实现重复键检查及受控未知字段处理；文档未声称标准库自动提供这些行为。一次应用层输出也没有夸称 OS 管道或进程崩溃下绝对原子。
5. **技术与平台依据：静态规则通过。** 新增证据字段和转换表是项目决策；授权／刷新成功作为可用证据明确依赖已核实 profile，未知有效期仍需当前版本运维确认。technology-evidence 未把 Seller、轮换、旧 Token 可用性升级为事实；1.2／1.6 尚未执行，SQ-3 不自动通过。本轮没有新增版本争议，也没有因为未重复在线核实未修改版本而产生冲突。

## 适用性与限制

技术通过表示方案与现有选型没有新的已知机制冲突。Transit 的 context／associated_data 能力引用仅支持设计路线，不证明最终键类型、编码、策略与身份配置已适用；1.2 必须按官方机制明确选择并给出完整字段/样例，1.6 在隔离后端取得实证。当前文档修订完成也不等于这些 Story、SQ-1 至 SQ-5、平台联调或开发就绪已经完成。
