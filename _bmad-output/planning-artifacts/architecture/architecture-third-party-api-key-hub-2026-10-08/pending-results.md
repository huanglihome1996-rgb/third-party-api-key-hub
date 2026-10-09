---
title: 平台待处置结果保护与实施交接
status: final
created: 2026-10-08
updated: 2026-10-08
binds: [AD-5, AD-6, AD-9, AD-10, SQ-3]
---

# 平台待处置结果保护

本文件是 [架构](ARCHITECTURE-SPINE.md) AD-6／AD-9 的强制 companion，解决 PM-05 的设计产出者与绑定边界。当前值保护和待处置结果保护是不同用途；禁止直接把暂存包当作当前凭据。

## 保护对象与所有权

平台请求领取前，application 持久化 operation_id、App、操作种类、已知发起者、授权会话／epoch（不适用时显式为空）、profile 版本和已知 App／resource／依赖对象 row_revision 快照。领取本身不推进领域版本。provider 只返回已核实的规范化结果，不写状态库。

平台结果尚未领域提交时，其全部规范化业务／管理秘密、店铺身份及期限放在同一受保护结果包内。多店铺结果保留各自平台身份和上下文，不在暂存阶段提前猜中心目标或最终 generation。授权码、原始 HTTP body 和未知扩展字段不进入暂存包、审计或错误。

包的认证绑定至少包含格式版本、purpose=pending-provider-result、operation_id、App、操作种类、session／epoch、profile 版本和完整已知依赖快照。保护后的包与其 operation 状态在业务事务原子保存；非敏感索引元数据不能代替解密后的绑定校验。当前凭据的 purpose=current-credential，仍绑定中心目标＋type＋generation；两个用途必须拒绝互换。

## 状态转换与提交

| 情况 | 行为 |
| --- | --- |
| 已领取／正在调用平台 | 持久化操作存在；重启时未终结操作阻止 App 普通自动轮换，不能重放平台请求。 |
| 完整结果成功保护并保存 | operation 标为 protected-result，可由获 manage 的处置用例读取。已撤权发起者不能提交领域变化，但服务仍可安全保管结果。 |
| 未能保护／保存完整结果或远端结果不明 | 标为 indeterminate，秘密仅在当前受控请求内处理，不能转明文落盘；按 profile 复核或受控重新授权，不能假设旧值可用。 |
| 本地条件提交 | 重查当前 manage、App、profile 版本、session／epoch、依赖 CAS 和中心身份归并。全匹配才将包内各类型重新保护为正确中心目标/type/新 generation，并在同一事务更新全部当前值、状态及 operation。保持停用和版本证据规则。 |
| 快照、epoch、profile 或资格不再符合 | 拒绝本地提交，保留受保护结果／待处置证据并继续阻止普通自动轮换；不得改写期望绑定迁就现状，也不得重复平台兑换。 |
| 已安全提交／平台复核后明确终结 | 当前凭据或复核结论与 operation 终结同事务；只有明确安全终结才解除对应阻塞。安全清理暂存包不删除仍需保留的无秘密操作历史。 |

一次处置仍需同一 App 排他协调。即使没有最后持久化的秘密，领取记录也必须使启动保护成立。OpenBao 不可用时保持阻塞；本地再次保护失败不能重新发起平台调用。最终提交消费者只接受对应 operation 的完整结果，禁止跨 App／operation／用途交换包。

## 任务产物与 SQ-3 子门槛

| 产出者 | 必须交付 | 消费者／门槛 |
| --- | --- | --- |
| Story 1.2：密文与结果保护设计 | contracts/secret-protection/SECRET-PROTECTION-CONTRACT.md 和 PENDING-RESULT-CONTRACT.md；固定包/schema/绑定编码、purpose 隔离、Seal/Open 期望绑定、完整规范化结果／版本快照、崩溃状态矩阵及暂存包保留／安全清理规则。使用虚构多店铺结果，不臆造 Seller 字段。 | SQ-3 结果保护设计子项；2.4／2.5／2.7 实施定稿前需可引用。 |
| Story 1.6：隔离验证 | validation-report.md 及可复跑 harness；验证当前值／待处置包用途互换、跨 operation/App、epoch/profile/依赖快照篡改拒绝，轮换后旧包可解、失败和模拟重启状态。 | SQ-3 结果保护隔离实证子项；依赖 1.2／1.3，无须等真实平台调用。 |
| Story 2.4／2.5／2.7：业务实现 | 按上述已验证产物实现安全保存、本地提交和受控处置，再检验真实 profile 的规范化结果与当前值生成。 | 原生产实现与真实联调门槛仍生效；harness 不代替业务崩溃恢复验收。 |

两份前序产物未完成或相关隔离用例失败时，2.4／2.5／2.7 的 SQ-3 子门槛不通过。不能因为已有当前凭据密文验证或本 companion 存在而自动勾选通过。本次将任务范围明确化，没有执行这些 Story。
