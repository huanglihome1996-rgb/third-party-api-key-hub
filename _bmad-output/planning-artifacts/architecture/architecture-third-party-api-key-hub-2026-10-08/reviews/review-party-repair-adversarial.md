---
title: Party 修订独立对抗审查
created: 2026-10-08
updated: 2026-10-08
status: reviewed-after-fix
lens: adversarial
verdict: pass-document-repair
---

# 结论

本次修订已实质闭合 PM-01 至 PM-05 的原问题。审查中发现一处由待处置结果绑定引入的架构规则冲突，主代理已修复，独立复读最新 spine 与 Story 1.2 后确认闭合；本轮文档对抗审查通过，无剩余发现。文档修订不等于实施或隔离验证已执行，不据此提高开发就绪状态。PM-06 的入口修改由总体一致性审查验收，不在本对抗状态审查内重复判断。

读取当前 ARCHITECTURE-SPINE.md、credential-state.md、response-compatibility.md、pending-results.md、Party PM-01..06 审查记录和 epics.md 对应验收场景。补读 PRD/SPEC 的手工导入与状态要求，避免把已采用的未知期限确认规则当作缺陷。本报告只修改自身；不更改已接受技术选择或其他代理的文件。

## AR-01 / P2（已修复）：CAS 修订号不绑定规则与待处置包依赖快照绑定互相冲突

- **位置：** ARCHITECTURE-SPINE.md:140；pending-results.md:15、19；epics.md:307。
- **冲突：** Spine 的状态版本约定无条件要求“用于 CAS 的修订号绝不写入密文绑定”。新增待处置契约却要求绑定“完整已知依赖快照”，其领取字段明确包含 App/resource/依赖对象 row_revision。Story 1.2 同样要求绑定依赖快照。因此无法同时遵守两个强制规则。
- **反例：** operation X 领取时记录 App revision=10、resource revision=4，收到多店铺平台结果后 Seal。结果保护实现者遵守 spine，排除这两个 CAS 修订号；结果提交实现者遵守 pending-results，要求在认证绑定中验证这两个号。二者对同一合法结果包产生不同验证结果。若改为不验证包内快照，则削弱新增结果绑定要求；若加入快照，则违反 spine 的无条件禁令。这是组件能独立作出不同选择的直接文档冲突，不是尚未执行计划的缺陷。
- **精确修复：** 将 ARCHITECTURE-SPINE.md:140 的禁令限定为 `purpose=current-credential`：当前凭据绑定稳定目标/type/generation，不加入可变 CAS 修订号；`purpose=pending-provider-result` 必须绑定领取时的完整已知依赖 CAS 快照及 session/epoch/profile。包的认证绑定证明它属于哪个领取上下文，最终提交仍另行比较当前领域版本，不能以认证通过代替当前 CAS。同步确认 Story 1.2 中“不加入 row_revision/authorization_epoch”的措辞只作用于当前凭据包。
- **复核标准：** 成功创建同时满足 spine 与 companion 的两类包；当前凭据仅因状态修订而变化时无需重加密；待处置包的 App/resource 修订号被替换时 Open 拒绝；原绑定合法但当前 row_revision 已变化时解密可成立、本地领域提交仍拒绝。修复的是上位文档范围，实际编码和隔离实证继续由 1.2/1.6 交付。
- **修复后复核：** 最新 ARCHITECTURE-SPINE.md:140 明确 `current-credential` 不含状态 CAS/会话版本，而 `pending-provider-result` 必须认证绑定领取时依赖 row_revision/适用 epoch，仍重新比较当前 CAS/epoch；epics.md:274 同步采用相同作用域。与 pending-results.md:15、19、28 完全一致。以上复核标准的文档要求可同时成立；尚未执行的运行实证继续由既有任务交付，不声称本审查已经运行包/harness。

## 原问题与组合反例核查

| 反例/边界 | 当前规则与验收依据 | 判断 |
| --- | --- | --- |
| 停用 g5 后仅刷新 g6，尝试 restore，包括重启 | credential-state:22、37、52、64；Story 1.18:928–931 | 事务恢复依据为空，generation 大小不能创建资格。PM-01 原问题已闭合。 |
| 停用后替换 g6，再刷新 g7，然后显式恢复 | credential-state:35、37、52、65；Story 1.18:933–936 | 合格沿革保留；g7 有当前可用证据才恢复；再次停用清空资格。 |
| 审计保存失败与备份快照恢复 | credential-state:15、22、52；AD-14；Story 1.18:930–931、3.7:1466–1467 | 资格来自业务事务，不能由审计缺口重建；恢复默认关闭取密与调度。 |
| A 已确认/valid，替换 B 未确认；B 未知期限 | credential-state:23–26、35、48–50、66；Story 1.13:728–731、1.15:805–807 | B 不继承证据，必须有当前版本确认；已知期限同样不得借旧证据。PM-02 原问题已闭合。 |
| 当前 valid 后暂时验证失败，或旧版本迟到 invalid | credential-state:39–41、68；AD-5/7；Story 2.6:1177–1192 | 同版本 incomplete 只更新尝试；旧响应的 generation/CAS 不匹配时不变更新值。 |
| g10 查询，g11 刷新成功后 g10 失效上报 | credential-state:56–60、67；Story 1.1:241–242、1.18:938–941 | expected_generation 必填，过时报告安全拒绝；主动 disable 单独表达停用当前值。PM-03 原问题已闭合。 |
| 旧 CLI 对新服务的合法可选非敏感字段 | response-compatibility:17–23、27、33–36；Story 1.1:244–247、1.16:848–855 | 固定允许扩展位置、重建白名单输出；错误对象和秘密条目仍严格。PM-04 原冲突已闭合。 |
| unknown credential type、重复字段、错误对象夹带值 | response-compatibility:19–23、34–35 | 必须拒绝，兼容扩展不扩大秘密输出范围。 |
| 共享 resource 更新多个安装，其中一类型未换值 | credential-state:15、36–37、69；AD-9；Story 2.7:1213–1221、1237–1240 | App 协调且同事务更新实际变化的新值，未换值类型不继承别处证据；刷新不得给任何类型创建恢复资格。 |
| 多店铺待处置包在最终中心目标尚未知时保存 | pending-results:15–19、28；Story 1.2:305–313、2.4:1089–1111 | 暂存使用 operation/App/平台上下文，最终才归并中心身份、产生 generation 并重新保护。PM-05 的产出者已明确；AR-01 修复后两种绑定规则一致。 |
| 跨 App/operation/用途交换，加密成功后崩溃 | pending-results:19、25–32、38–39；Story 1.6:467 之后与 2.4/2.5 | 认证绑定、持久化领取和启动阻塞已要求；不以未执行 harness 作为新缺陷。 |
| 撤权、epoch/CAS/profile 过时，或 KMS/DB 无法保存 | pending-results:26–32；AD-5/9；Story 2.5:1132–1145 | 安全保管和有资格处置分开；不符合条件不能提交，不改期望绑定迁就现状，不重复平台调用，不落明文。 |

## 未计为缺陷的边界

- 目标 Seller 市场/App、真实轮换/可选验证和多店铺共享 grant 的具体平台语义已有 SQ-1 及 1.5/2.1/2.8 承接，本轮没有从通用 OAuth 猜测它们。
- 暂存包编码、保留与安全清理、崩溃矩阵和受控恢复的精确实现已交接 1.2/1.6/2.4/2.5；规则与产物有清楚所有者，尚未交付实证不等于文档自身有新漏洞。
- 未知期限换值后需要重新确认会使某些平台值暂不可用，这是当前采用的证据规则，并非旧确认应继承的理由。
- App 协调的具体锁实现、后台重启和生产 KMS 行为没有业务代码或运行证据，本轮结论限于文档一致性及组合状态契约。

本轮仅产生 AR-01 且已独立复核闭合；未发现遵守上述补充规则后仍可通过普通刷新绕过停用、继承旧确认或用旧失效报告停用新值的状态反例。
