---
title: 六项文档审查问题修订与复核
created: 2026-10-08
updated: 2026-10-08
status: complete
implementation_readiness: FAIL
---

# 六项文档审查问题修订

用户要求“把问题按照建议都处理一下”。本次将 [初审 PM-01 至 PM-06](2026-10-08-document-review.md) 的建议落实到现行 PRD、架构、SPEC、实施门槛和 Story，保留原编号及首期边界。文档规则已修订，真实设计产物、实施和运行证据仍由规划任务交付，不将本次修订计为这些任务执行完成。

六项文档问题已关闭。下列规则与后续验收已同步，并经三份独立审查及确定性检查复核；开发就绪仍为FAIL。

## 六项闭环证据

| 问题 | 修订结果 | 当前权威证据 | 相关验收 |
| --- | --- | --- | --- |
| PM-01 恢复来源证明 | 用recovery_basis_generation/operation与值同事务保存合格替换/重授权；刷新不创建资格，后续刷新可保留资格，再次停用清空；restore复核当前版本和完整条件。 | [凭据状态契约](../../planning-artifacts/architecture/architecture-third-party-api-key-hub-2026-10-08/credential-state.md) | Story1.18：仅刷新恢复拒绝、替换后刷新保留资格、再次停用、重启后相同结果。 |
| PM-02 换代证据 | confirmed_generation、validation_generation明确证据作用版本；换值不继承旧确认/验证/期限，临时验证失败另记尝试并保留同版本确定证据。 | [状态所有权与事件转换表](../../planning-artifacts/architecture/architecture-third-party-api-key-hub-2026-10-08/credential-state.md) | 1.13/1.14/1.15及2.4/2.6/2.7覆盖替换未确认、迟到验证和共享刷新资源。 |
| PM-03 旧值失效上报 | 取密每type状态返回同快照generation；mark-invalid要求使用时expected_generation，旧版本报告拒绝不改新值；主动disable独立处理当前目标。 | [PRD FR-7/FR-10](../../planning-artifacts/prds/prd-third-party-api-key-hub-2026-10-08/prd.md)和[状态契约](../../planning-artifacts/architecture/architecture-third-party-api-key-hub-2026-10-08/credential-state.md) | 1.1/1.16/1.18覆盖查询旧值→刷新→失效报告及主动停用。 |
| PM-04 响应兼容 | 请求严格；仅指定响应元数据位置允许非敏感可选扩展；旧CLI按已验证字段重建输出，不透传未知字段，凭据/错误对象仍严格。 | [响应兼容契约](../../planning-artifacts/architecture/architecture-third-party-api-key-hub-2026-10-08/response-compatibility.md) | 1.1/1.16覆盖旧CLI＋新增可选状态字段成功，以及未知秘密/重复项/错误夹带值失败。 |
| PM-05 结果保护产出者 | 区分current-credential与pending-provider-result；结果包认证绑定operation/App/epoch/profile/依赖快照。1.2设计、1.6隔离验证，2.4/2.5/2.7明确消费子门槛。 | [待处置结果保护契约](../../planning-artifacts/architecture/architecture-third-party-api-key-hub-2026-10-08/pending-results.md)及[实施门槛](../../specs/spec-third-party-api-key-hub/implementation-gates.md) | 1.2/1.6加入用途/跨operation/App/快照交换拒绝、旧包解密、模拟崩溃；业务任务补实际安全保存/本地条件提交。 |
| PM-06 文档入口 | README指向SPEC、强制companions、实施门槛、CE规划及FAIL判断；早期简报标明历史输入。PRD Q表同步已接受架构与剩余SQ路由。 | [README现行入口](../../../README.md#现行文档入口) | 从首页即可定位当前基线与未完成门槛，不把final理解成实现通过。 |

## 验证记录

- 架构机械lint通过，0 findings；20份相关Markdown的94处本地链接通过，AD14/FR12/CAP12/Story34编号保持；157组完整Given/When/Then/And场景、167条向前依赖、覆盖表及CE/SPEC来源哈希通过。实际输出见 [确定性回执](document-repair-validation.json)，原输入修订差异见 [差异记录](document-repair.diff)。
- 三个独立复核均PASS、无剩余发现：[对抗状态审查](../../planning-artifacts/architecture/architecture-third-party-api-key-hub-2026-10-08/reviews/review-party-repair-adversarial.md)、[来源/rubric](../../planning-artifacts/architecture/architecture-third-party-api-key-hub-2026-10-08/reviews/review-party-repair-rubric.md)、[技术可实现性](../../planning-artifacts/architecture/architecture-third-party-api-key-hub-2026-10-08/reviews/review-party-repair-technology.md)。修复了当前密文“不绑定状态版本”与暂存包“必须绑定操作快照”的作用域冲突，并把安全本地提交纳入AD-9的终结路径。
- 原架构图表示的组件与连接关系没有变动，图源、HTML与既有图像/浏览器回执保持原产物；当前契约细化由新增强制companion承载，不把旧图像证据当作新状态规则的验证。
- SPEC按既有memlog及更新的采用文档重新提炼，仍为12项CAP、4份顶层companions、SQ-1至SQ-5；架构继续以AD-1至AD-14承载规则，Story仍为34条规划。

## 完成边界

本次只处理六项文档问题及其交接、反例和验证回执，不执行1.1/1.2/1.6等实际设计或隔离验证任务。完整CLI schema、保护包编码、目标Seller profile、Linux/OpenBao/平台实证和性能/恢复验收仍未取得。现有implementationReadiness/ready标记保持FAIL/false，不生成Sprint状态或派发实施。
