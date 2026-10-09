---
title: 凭据版本证据与状态转换契约
status: final
created: 2026-10-08
updated: 2026-10-08
binds: [AD-2, AD-5, AD-7, AD-8, AD-9, FR-6, FR-7, FR-8, FR-9, FR-10]
---

# 凭据版本证据与状态转换

本文件是 [架构](ARCHITECTURE-SPINE.md) AD-7 的强制 companion，依据用户要求处理 PM-01／PM-02／PM-03 的指令固化。规则已采用；实现和运行验证仍由对应 Story 交付。

## 状态所有权

每份逻辑凭据以中心目标＋type 唯一定位。application 在同一业务事务保存当前密文、generation、状态和证据；操作审计不能作为恢复资格的唯一证明。共享刷新资源仅更新实际换值的凭据，各自推进 generation，未换值的类型不继承其他类型的证据。

| 字段／证据 | 规则 |
| --- | --- |
| generation | 当前秘密值版本，初值 1；成功导入替换、重新授权、刷新实际换值时递增。密钥重新加密不改变它。 |
| value_origin | 当前值来源：manual_import、manual_replace、authorization 或 refresh。用于状态转换，不代替恢复资格证据。 |
| disabled、disabled_generation | 停用决定及该决定针对的值版本。新的明确停用／当前值失效标记记录当前 generation，并清空恢复资格。 |
| recovery_basis_generation、recovery_basis_operation_id | 停用后成功明确替换／重新授权的最近合格值版本和业务 operation 引用；两者同时为空或同时有值。与值更新同事务保存，不能依赖可能丢失的审计。 |
| confirmed_generation | 最近明确运维确认适用的当前 generation，未确认为 null；confirmed_by_operator 仅由它是否等于当前 generation 派生，不另存矛盾布尔值。 |
| validation_generation、validation_result | 最近确定的平台验证证据版本及结果 valid／invalid；尚无确定证据时均为空。证据只有与当前 generation 一致才适用。 |
| last_validation_attempt | 最近一次验证尝试的 generation、时间、结果 valid／invalid／incomplete；暂时故障更新该尝试而不覆盖同版本既有的确定证据。 |
| expiry_known、expires_at | 已知期限必须为本次值的绝对时间；未知为 false／null。换值不得继承旧值期限，运维确认不延长期限。 |

## 事件转换

本表只描述成功提交。失败／CAS 冲突不改变当前密文和领域状态；具有外部副作用的失败仍按 AD-9 保护结果并阻止盲重放。

| 事件 | 值版本与证据 | 停用和恢复资格 |
| --- | --- | --- |
| 首次手工导入 | generation=1，来源 manual_import；确认、验证证据为空。若同次输入显式确认，则 confirmed_generation=1。期限按本次输入/profile。 | 初始无停用、无恢复资格。 |
| 明确手工替换 | generation 递增，来源 manual_replace；清空旧确认、确定验证和验证尝试，按本次值重新设置期限。允许同次输入显式确认新版本。 | 保留 disabled/disabled_generation；若已停用，设置恢复资格为本次新版本/operation。 |
| 重新授权成功提交 | 实际换值的类型 generation 递增，来源 authorization；清空旧确认/验证证据，期限与可用性依据本次已核实规范化结果，不伪装为另行平台验证成功。 | 保留停用；对已停用且实际换值的类型设置合格恢复版本/operation。 |
| 后台刷新成功提交 | 实际换值的类型 generation 递增，来源 refresh；清空旧确认/验证证据，采用本次已核实期限与获取结果。 | 保留停用和已有恢复资格，绝不新建或推进恢复资格；不因刷新取消合格替换的沿革。 |
| 运维确认 | 必须指定 expected_generation；匹配当前值后设置 confirmed_generation，推进状态修订而不推进 generation。 | 不清除停用、不创建恢复资格。 |
| 平台验证 valid | 只接受操作快照仍匹配的当前 generation；保存同版本 valid 证据和验证尝试，不改变秘密版本或期限。 | 不解除停用、不创建恢复资格。 |
| 平台验证 incomplete | 保存同版本未完成尝试；保留同版本既有确定证据及运维确认。 | 不恢复过期、失效或停用值。 |
| 平台验证 invalid／匹配版本的业务失效报告 | 保存当前值明确失效证据或业务原因，执行当前版本停用，不推进 generation。 | 记录当前 disabled_generation 并清空恢复资格。 |
| 运维主动 disable | 在写门内读取并停用当前 generation；重复的明确停用仍建立新的停用决定。 | 记录当前 disabled_generation，清空此前恢复资格。 |
| 显式 restore | 对指定 expected_generation 复核当前值有效性、完整映射、manage、CAS 和下述恢复资格。 | 全部满足才清除 disabled、disabled_generation 和恢复资格；不改变 generation。 |
| Transit 密钥轮换／重新加密 | 保持 generation 和所有业务证据，按 AD-6 复核原绑定；后端密钥版本可变化。 | 不更改停用或恢复资格。 |

## 可用性与恢复

任何返回还须满足完整映射、read 资格、组合必需字段和密文绑定。每个必需类型须未停用，未被当前版本确定证实失效，且具备当前版本可用证据：明确运维确认、当前 valid 验证，或已核实 profile 明确允许且本次授权／刷新完整取得的成功结果。未确认的手工导入不能借旧版本证据可用。

已知期限须 expires_at > now；未知期限仍要求 confirmed_generation 等于当前 generation，不由平台获取成功推定为永不过期。profile 不得用“未知／未核实”能力代替成功获取证据；真实获取规则仍受 SQ-1 约束。

restore 要求 recovery_basis_generation > disabled_generation，且 recovery_basis_generation <= 当前 generation，并具有本次停用之后的合格替换／重新授权 operation 证据。资格只沿该逻辑凭据的连续成功值更新沿革保留；再次停用／明确失效将其清空。当前版本即使晚于停用，也不能单独证明具备恢复资格。旧快照恢复后按 AD-14 关闭取密与调度，先复核这些证据，不从审计缺口重建资格。

## 业务失效报告与版本引用

成功取密的 status.app 和 status.shop 每个 type 条目包含正整数 generation，与该条目业务值来自同一读门快照。状态查询同样返回此非敏感引用。

mark-invalid 的输入必须包含中心目标、type、expected_generation 和枚举原因；调用者须 manage。它表达“我使用的这一版本明确失效”，不以秘密值作为比对材料。expected_generation 不匹配时返回 input_invalid 和安全冲突说明，不变更当前状态；不能自动填成服务器刚读到的版本。服务端 CAS 仍负责检查请求快照之后的并发变化。

运维主动 disable 表达“停用当前目标”，保持独立操作含义；不要求调用者先取明文。确认与 restore 均显式指向其正在处理的 expected_generation，拒绝过时意图。

## 必须交付的反例验收

- 停用 g5→仅刷新 g6→restore 拒绝；重启后结果一致。
- 停用 g5→明确替换 g6→刷新 g7→明确确认 g7（若未知期限）→restore：其余条件满足时可恢复。再次 disable 后旧恢复资格不能复用。
- A 已确认或验证→替换 B 未确认：B 不继承 A 的证据；同次显式确认 B 与随后确认 B 均可按当前版本建立证据。
- g10 查询→刷新 g11→报告 g10 失效：g11 不被停用；报告当前 g11 则停用。主动 disable 仍停用当前值。
- 同版本 valid 验证后出现暂时故障：记录 incomplete 尝试而保留 valid 证据；旧版本迟到 valid／invalid 响应均不能改变新值。
- 共享 resource 更新多类型及未换值类型、密钥重新加密、崩溃重启、备份恢复分别检查证据版本、停用决定和原子性。
