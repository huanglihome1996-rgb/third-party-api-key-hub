---
title: 能力追溯与实施门槛
status: final
created: 2026-10-08
updated: 2026-10-08
---

# 能力追溯与实施门槛

## 契约读取与当前状态

[SPEC.md](SPEC.md) 的 CAP-N 对应 [PRD](../../planning-artifacts/prds/prd-third-party-api-key-hub-2026-10-08/prd.md) FR-N；PRD 保留逐项验收、术语、NFR-1 至 NFR-6、SM-1 至 SM-8／SM-C1／SM-C2，以及后续 PF-1／PF-2。[技术补充](../../planning-artifacts/prds/prd-third-party-api-key-hub-2026-10-08/addendum.md) 保留平台研究的适用性限制及技术交接责任。[架构](../../planning-artifacts/architecture/architecture-third-party-api-key-hub-2026-10-08/ARCHITECTURE-SPINE.md) 的 AD-1 至 AD-14、Consistency Conventions、Stack、Structural Seed、Operational Targets 和 Deferred 均为合同组成部分；架构图保留在该 companion 中；架构强制读取的 credential-state.md、response-compatibility.md、pending-results.md 承载本轮版本证据、兼容和待处置保护修订。

用户已接受全部架构决策；PRD/addendum 中“后端未选定／技术待设计”描述的是架构之前的交接阶段，采用当前 final 架构中的已确认决定。完整 CLI 与平台协议、精确部署与恢复操作仍未完成，不把已接受架构理解为这些验证通过。既有文件不由 bmad-spec 修改；后续发现真正冲突时记录并交由相应上游技能更新。

## 能力追溯

| CAP（同号 FR） | 架构规则 | 产品成功判据 | 相关细化门槛 |
| --- | --- | --- | --- |
| CAP-1 | AD-2、AD-4、AD-11、AD-13 | SM-1 | SQ-1、SQ-2 |
| CAP-2 | AD-2、AD-4、AD-5、AD-10 | SM-1、SM-3 | SQ-1 |
| CAP-3／4 | AD-3 至 AD-5 | SM-2 | SQ-2 |
| CAP-5 | AD-5、AD-6、AD-9 至 AD-11 | SM-3 | SQ-1、SQ-3、SQ-4 |
| CAP-6 | AD-3 至 AD-7、AD-13 | SM-4 | SQ-2、SQ-3 |
| CAP-7／8 | AD-5 至 AD-7、AD-9、AD-11 | SM-4 | SQ-1、SQ-3 |
| CAP-9 | AD-3 至 AD-8、AD-13 | SM-5 | SQ-1、SQ-2、SQ-3 |
| CAP-10 | AD-8、AD-13 | SM-4、SM-5 | SQ-2 |
| CAP-11 | AD-3、AD-14 | SM-6 | SQ-4 |
| CAP-12 | AD-3、AD-12 至 AD-14 | SM-6 | SQ-2、SQ-4 |

全部能力受 NFR-1、SM-C1／SM-C2 约束；NFR-2 至 NFR-5 的跨功能要求按 PRD 与架构映射落实，部署与扩展另按 SM-7／SM-8 验收。SQ-4 的主机权限和部署验收也覆盖所有已发布命令；SQ-5 覆盖完整服务的运行与恢复，不局限于某个 CAP。本表是路由，不缩小 companion 中的验收范围。

## 相关 Story 编写前

| 门槛 | 责任角色 | 必须形成的证据 |
| --- | --- | --- |
| SQ-1 平台契约 | 平台接入设计者 | 目标市场／App 类型、官方版本与适用性、Seller 授权与来源校验、店铺身份／shop_cipher、秘密用途与必需组合、Token 期限与轮换、共享 refresh resource、验证／失效／安全重试分类及受控联调计划；按 AD-9 至 AD-11 处理不确定副作用。 |
| SQ-2 CLI 契约 | CLI 设计者（1.1） | AD-13完整命令/schema/样例、错误优先级、stdin/TTY/取消；每type输出generation，mark-invalid/确认/restore的expected_generation与主动disable语义；按response-compatibility.md验证旧CLI元数据扩展成功、未知秘密拒绝及无秘密输出；断连后非敏感当前版本/状态复核，不依赖审计唯一证明、不盲重放。 |
| SQ-3 密钥保护 | 安全存储及部署设计者 | 1.2交付SECRET-PROTECTION-CONTRACT.md与PENDING-RESULT-CONTRACT.md，固定当前值/待处置包编码及用途/operation/App/epoch/profile/依赖绑定；1.6分别取得两用途交换拒绝、旧密文解密、失败/模拟重启隔离实证。2.4/2.5/2.7定稿前必须消费结果保护子项，普通当前密文验证不能替代。1.3/1.6负责AppRole发放/轮换/回收、Token续期/重新认证；3.1/3.4/3.5负责独立密钥版本备份恢复。 |
| SQ-4 部署与采集 | 部署设计者 | 目标 systemd 指令、账户与策略权限、仅回调 TLS／证书入口、安全告警、未到达服务的连接拒绝采集、业务／审计一致备份与一年保留；Linux 实际验收，无 argv／请求体／秘密采集。 |
| SQ-5 运行与恢复 | 运行及恢复设计者 | 10 并发与健康 KMS 下 p95 ≤2s 的测量口径、每日一致备份／RPO ≤24h、人工恢复 RTO ≤4h 的故障条件及演练步骤。恢复默认关闭取密和调度，复核映射／账户／平台当前值后显式开放。 |

状态转换以架构 credential-state.md 为验收契约：1.13/1.14保存新值时重置旧证据，1.15确认匹配版本，1.18以事务内恢复资格及版本失效反例验收，2.4/2.6/2.7沿用；文档补齐不代表这些任务已执行。

责任角色不是已分配的具名人员。未满足门槛的相关 Story 不视为可实施；非平台路径的手工导入不以真实授权或可选平台验证作为普遍前置条件，但仍须落实该路径需要的 CLI、字段用途、存储、权限和状态契约。真实平台请求、部署或第三方资源变更不由本规格输出自动授权。

## 既有问题编号的承接

| 原编号 | 当前处理 |
| --- | --- |
| Q-1、Q-2、A-1 | 工作方式、选择与 JSON 产品基线已确认，不重新提问。 |
| Q-3、Q-5、Q-10 | 产品行为和 AD-4／AD-8／AD-13 已决定；完整 CLI 细化为 SQ-2，平台字段／支持性为 SQ-1。 |
| Q-4 | AD-3 的 UID、账户白名单和 read／manage 已接受；实际部署验证归 SQ-4。 |
| Q-6 | AD-5／AD-7／AD-9／AD-10 的后台调度、协调、版本和恢复规则已接受；平台轮换与调度参数归 SQ-1。 |
| Q-7、A-2 | 归 SQ-1；既有官方资料只作线索，不提升为已核实 Seller 契约。 |
| Q-8 | AD-12 的独立审计、字段、检索和安全告警已接受；保留／采集／备份部署归 SQ-4。 |
| Q-9 | 数据库、OpenBao、Linux 目标及运行目标已接受；具体实施验证归 SQ-3 至 SQ-5。 |
| Q-11、PF-1、PF-2 | 后续 Vaultwarden 自动推送与平台内权限方向保留；范围、条目映射、首次同步、时限、重试／顺序／冲突、撤销／删除传播在集成启动前另行设计，不阻塞首期。 |

后续 Story 应引用 CAP／FR／AD，并落实适用的 NFR、SM 和 SQ。此规格不包含 stories.yaml，也不指定未经用户确认的 spec_checkpoint、done_checkpoint 或开发派发备注。
