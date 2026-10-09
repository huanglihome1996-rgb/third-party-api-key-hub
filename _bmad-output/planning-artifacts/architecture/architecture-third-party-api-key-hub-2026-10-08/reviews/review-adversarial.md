---
title: 对抗性一致性复核
created: 2026-10-08
updated: 2026-10-08
verdict: pass
---

# Verdict

**当前复核通过。** 以下保留初审的两处历史发现，均已解决。本轮不重复 App/共享秘密调度、审计入口及 Socket 目录问题。复核已重新读取 AD-1 至 AD-14 与版本约定。

## 初审 Findings（均已解决）

1. **P1 — `generation` 同时承担密文版本与状态 CAS，但没有一致的作用域和推进规则。** 精确位置：[spine:55](../ARCHITECTURE-SPINE.md#L55)、[spine:73](../ARCHITECTURE-SPINE.md#L73)、[spine:79](../ARCHITECTURE-SPINE.md#L79)、[spine:85](../ARCHITECTURE-SPINE.md#L85)、[spine:103](../ARCHITECTURE-SPINE.md#L103)。构造两个遵守现有字面规则的单元：凭据存储单元按 AD-6 把密文绑定每份凭据的 generation；停用/验证单元按 AD-2/AD-5 把 generation 当所有更新的 CAS 修订号，在仅修改 disabled/validation_result 时推进。停用后再恢复，旧密文仍绑定旧 generation，解密失败；若维护单元反过来只推进安装 generation，凭据单元只推进各 type generation，两者也会比较不同的“目标版本”，使迟到结果拒绝或错误覆盖。建议明确不可变的秘密值版本与状态/安装修订号各自拥有者、哪些操作推进、平台结果所校验的快照，以及 disabled_generation 究竟引用哪一版本；密文绑定使用秘密值版本，不能随纯元数据更新而失配。

2. **P2 — 外部店铺的中心身份与平台身份未分开定义，适配器和映射可选择不同命名空间。** 精确位置：[spine:67](../ARCHITECTURE-SPINE.md#L67)、[spine:103](../ARCHITECTURE-SPINE.md#L103)、[spine:133](../ARCHITECTURE-SPINE.md#L133)。构造两个下层单元：TikTok 适配器把原始 provider shop ID `"123"` 作为 external_shop_id；映射仓储认为 external_shop_id 是跨平台的中心稳定 ID，直接对 `"123"` 建品牌归属。二者均满足字符串稳定标识、唯一安装及分离 shop_cipher 的现有规则，但第二个平台也返回 `"123"` 时会与已有店铺混合或被唯一性错误拒绝，违背 NFR-4 的无需改核心模型。建议声明中心外部店铺 ID 全局唯一，平台身份按 `(platform, provider_shop_id)` 唯一，安装与映射只引用中心 ID；跨 App 同一平台店铺归并仍由中心用例执行，shop_cipher 属适配器/App 上下文而非身份。

## 已收敛的攻击路径

单一 application 变更入口、一致性门、当前凭据唯一约束、整体失败及停用恢复规则封住了直读 DB、部分秘密返回、解除映射后新查询读取旧链等分歧。平台失败分类、回调协议、DTO schema 等已经明确 Deferred 且禁止对应 Story 在契约未落实时实施，不将其合理延迟重复计为缺陷。新技术建议尚待用户接受，与已确认 PRD 无直接冲突。

## 2026-10-08 修复后复核

- **P1 resolved**：当前 AD-2/AD-5 及版本约定（55、73、137—139 行）区分每份逻辑凭据的秘密值 `generation`、对象及 App 聚合 `row_revision`、授权意图 `authorization_epoch`；纯元数据不会改变密文绑定，异步结果使用显式状态快照。
- **P2 resolved**：当前 AD-4/AD-10（67、103 行）明确中心全局 ID、平台身份二元组、application 归并所有权及 App/安装上下文的 shop_cipher。
- 未发现实际剩余的 PRD 矛盾或可由合规下层单元构造的上述分歧。当前 verdict 为 pass；这表示架构草案通过一致性复核，不表示新建议已被用户接受、Deferred 契约已完成或软件已通过实施验收。
