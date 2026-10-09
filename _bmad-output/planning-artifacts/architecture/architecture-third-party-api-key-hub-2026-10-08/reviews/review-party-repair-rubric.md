---
title: Party 修订独立 Rubric 与来源保全复核
created: 2026-10-08
updated: 2026-10-08
verdict: PASS
implementation_readiness: FAIL
---

# 独立复核结论

六项 Party 建议已落实到现行产品、架构、规格、规划与首页入口。复核过程中发现两处残留契约冲突，维护者修订后已重新读取并确认消除；当前没有阻断文档修订交付的未解决问题。PASS 仅表示本次文档修订符合审查建议，不表示 Story 已执行或开发就绪。

## 范围与方法

独立读取 ARCHITECTURE-SPINE.md、credential-state.md、response-compatibility.md、pending-results.md、PRD/addendum、SPEC/implementation-gates、epics.md、README、CE-VALIDATION 与原 PM-01 至 PM-06 报告。逐条检查规则、反例验收、来源继承和任务依赖；按 repair-baseline.json 指定的修订前文件逐一比较 AD、FR、CAP 与 Story 标题编号序列，全部一致。未调用真实平台、配置生产身份、运行业务验收或把 Deferred 事项当作本轮缺陷。

## 六项建议验收

| 建议 | 当前权威证据 | 复核结果 |
| --- | --- | --- |
| PM-01 恢复资格来源 | AD-7、credential-state 的 recovery_basis_generation/operation、FR-8/CAP-8、1.18/2.5/2.7 | 与当前值同事务保存；仅刷新不建资格，替换后刷新保留沿革，再次停用清空，重启/恢复反例明确。不是只比较 generation 或依赖可丢失审计。 |
| PM-02 换值证据 | credential-state 事件表、FR-6/7、CAP-7、1.13/1.14/1.15/2.4/2.6/2.7 | 导入、替换、重新授权、刷新、确认和验证分别有规则；换值清旧确认/验证，平台取得不伪装独立验证；未知期限每次换值重新确认，暂时故障保留同版本确定证据。 |
| PM-03 旧值失效报告 | credential-state 的版本引用、FR-7/10、1.1/1.18 | 查询同快照 type/generation；mark-invalid 必须 expected_generation，过时报告 input_invalid 且状态不变；主动 disable 独立，确认/restore 也拒绝过时意图；没有远程上报或秘密比对。 |
| PM-04 旧 CLI 兼容 | AD-13、response-compatibility、FR-10/CAP-10、1.1/1.16 | 请求仍严格，成功响应只在指定非敏感元数据位置兼容扩展；旧 CLI 校验已知字段并重建输出，不透传未知值；顶层/凭据/错误严格，重复字段、未知秘密、改变已知类型均拒绝。原 1.16 无差别拒绝额外字段已消除。 |
| PM-05 结果保护产出者 | AD-6/9、pending-results、SQ-3、1.2/1.6/2.4/2.5/2.7 | 1.2 明确两份编码契约及验证矩阵，1.6 明确独立两用途实证；2.4/2.5/2.7 共同消费结果保护子门槛。暂存包绑定 operation/App/epoch/profile/依赖快照、用途隔离和完整结果；保存/崩溃/条件提交/过时拒绝/清理有契约，不靠当前凭据测试代替结果包实证。 |
| PM-06 文档入口 | README 当前状态/现行文档、历史简报入口说明 | 从首页到 SPEC、产品/架构 companions、实施门槛、epics/CE 均可定位；明确 34 条仅规划、业务实现和实证未开始、整体开发就绪 FAIL。历史简报不再被描述为现行选择依据。 |

## 复核中已消除的冲突

1. **原 P2：1.2 的版本绑定范围不明确。** epics.md 的旧句“row_revision 与 authorization_epoch 不加入密文绑定”与本 Story 新增待处置包 epoch/依赖快照绑定相冲突。当前第 274 行已明确 current-credential 不加状态版本，而 pending-provider-result 绑定领取快照/适用 epoch；与 companion 和 spine 版本约定一致。
2. **原 P2：AD-9 阻塞解除遗漏本地成功提交。** 旧句仅允许平台复核/受控重授权解除，排除了已保护结果安全本地提交。当前 AD-9 第 99 行已加入“已保护完整结果通过本地条件提交安全终结”路径；与 pending-results 状态表和 2.5 的本地处置验收一致，仍不允许重放平台请求。

## 来源保全与门槛边界

- AD-1 至 AD-14、FR-1 至 FR-12、CAP-1 至 CAP-12 和 34 条 Story 的编号序列与修订前一致；新细化通过原 AD 和强制 companions 承接，没有替换原需求编号。
- 首期仍为 Linux 主机 CLI、TikTok、OpenBao、单实例及既定完整组合；没有新增 Web 管理、远程取密、对象级权限、Vaultwarden 首期实施或业务 API 代理。
- 错误仍只有 error/message、stdout JSON、成功 0/失败 1；完整响应后才输出业务秘密，I/O 故障边界明确，没有借兼容扩展放行未知秘密。
- 1.2 是设计产出，1.6 是隔离实证，2.4/2.5/2.7 是业务实现消费者；隔离 harness 不依赖未来业务表或真实平台，且不冒充业务崩溃恢复/真实 Seller 验收。
- epics frontmatter implementationReadiness: FAIL、implementationReady: false，CE 与 README 一致。planned-gated 和 approved-for-planning 没有被升级为 ready-for-dev 或已完成；SQ-1 至 SQ-5 的实际证据仍须后续执行取得。

## 非阻塞措辞建议

PRD Q-3/Q-6/Q-8 表仍有架构前的“错误编码、调度/并发/是否即时刷新、审计存储待设计”表述，而 addendum 称 Q 表维护当前状态。入口与 SPEC/implementation-gates 已明确其历史角色和现行架构优先，因此不构成未决产品/架构冲突；可将这些行同步为“相关选择已由 AD-5/9/12/13 采用，剩余完整 schema/profile/部署证据按 SQ 路由”，避免截取 Q 表时重新打开已确认决定。此建议不要求扩展任务或重新向用户确认。

本报告不把仍待形成的详细编码、Linux/OpenBao/真实平台环境、运行测量或恢复演练记为本轮修订未完成。这些条件已有明确产出者、消费者与 Deferred 复核时点，保留开发就绪 FAIL 是正确状态。

## 最终定点复核：措辞建议已关闭

2026-10-08 按维护者通知仅重新读取 PRD 当前 Q 表（第 366–384 行），未扩大审查范围。Q-3 已区分已采用的三组 JSON、稳定错误码/优先级与完整 schema/样例；Q-6 已明确 AD-5/7/9/10 的 App 协调、CAS/epoch、持久化调度、不即时刷新及结果恢复为已采用；Q-8 已明确 AD-12 的审计存储/字段/检索/告警选择，剩余细化指向 SQ 和产出任务。同步修订的 Q-4/Q-10 也已区分采用规则与精确配置/Linux 实证、CLI 参数工作。

上述非阻塞措辞建议现已关闭，保留原发现作为复核历史。最终文档判定 **PASS，无本轮剩余问题**；开发就绪继续为 **FAIL**，实际设计产物、实现与运行验证仍按既有 SQ 门槛执行。
