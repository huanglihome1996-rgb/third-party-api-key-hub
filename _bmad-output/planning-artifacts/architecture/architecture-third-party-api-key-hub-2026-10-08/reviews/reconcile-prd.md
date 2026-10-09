---
title: PRD 输入对齐复核
created: 2026-10-08
updated: 2026-10-08
verdict: pass
---

# PRD 对齐结论

**当前复核通过；下列两处初审发现均已解决，原描述保留作历史。** FR-1 至 FR-12、NFR-1 至 NFR-6 均有对应 AD 或 Deferred；未发现新推荐直接推翻已确认产品行为。SQLite、OpenBao、全目标 read 权限、运行指标等均明确为待接受建议，不能视为已获用户批准。TikTok 未核实能力与 Vaultwarden 后续范围正确保留。

1. **P1 — 刷新排他范围只写安装授权，遗漏 App 级可刷新凭据。** PRD FR-7 明确保存 App 级与安装授权级凭据；FR-8 要求自动维护所有支持刷新的凭据，没有把 App 级排除（[PRD](../../../prds/prd-third-party-api-key-hub-2026-10-08/prd.md#fr-7-凭据保存与验证)、[FR-8](../../../prds/prd-third-party-api-key-hub-2026-10-08/prd.md#fr-8-凭据刷新与撤销)）。[AD-9](../ARCHITECTURE-SPINE.md#ad-9--刷新与外部副作用) 的“同安装同一时刻最多一个平台维护操作”仅覆盖安装；多个安装共享的 App 凭据可能被并行轮换，或被实现者排除调度。将 operation 的排他键定义为实际轮换的凭据所有者／共享秘密目标，明确 App 级与安装级均按 adapter capability 调度，避免新增平台时两套实现选得不兼容。

2. **P2 — 审计范围继承正确，但查询入口和边界前拒绝事件尚未明确归属。** PRD FR-12 要求成功与失败事件都可检索；FR-11 包括未授权账户的失败。首期管理全走 CLI，且 [AD-1](../ARCHITECTURE-SPINE.md#ad-1--共享核心与进程职责) 禁止 CLI 直读数据库。[AD-12](../ARCHITECTURE-SPINE.md#ad-12--非阻断审计与安全诊断) 只描述存储及业务结果后的写入；Capability Map 只列 audit 仓储/journald，尚未规定审计检索用例，或 local adapter 在身份／权限／schema 拒绝时如何记录事件。建议声明服务拥有受控 CLI 审计检索用例，并让请求边界拒绝进入同一安全审计端口；字段/命令语法继续交 CLI Story，身份不可取得时记录明确未知值、不得虚构账户。这样落实 [FR-12](../../../prds/prd-third-party-api-key-hub-2026-10-08/prd.md#fr-12-基础访问审计)，同时避免凭据拒绝在 application 之前被静默丢掉。

以上为输入承接边界的补充，不要求扩写完整数据库 schema、命令树或平台协议。其余 Deferred 均带责任与复核条件，可保留到对应 Story 编写前。

## 2026-10-08 修复后复核

- **P1 resolved**：AD-9 当前 97 行明确 App 级与安装级同一维护入口、共享 refresh_resource_id、共同 App 级远端副作用协调，以及资源依赖值的原子更新。FR-8 的 App 级支持能力不再被遗漏。
- **P2 resolved**：AD-12 当前 115 行声明 application 审计用例、manage 资格的 CLI audit list、本地/回调边界拒绝记录、客户端自报拒绝及未到达服务的系统事件采集；未知身份/目标不虚构，审计失败保持非阻断。
- 本轮重新读取当前 spine 后，没有实际剩余的需求矛盾或输入承接遗漏。新架构选择仍为 draft/assumption，须由用户批量接受；TikTok、完整 CLI schema、部署精确配置等仍按 Deferred 在对应 Story 前落实。
