---
title: 已接受架构方案与决策依据
status: accepted
created: 2026-10-08
updated: 2026-10-08
---

# 已接受架构方案

用户于 2026-10-08 回复“全部采用”，一次接受下列 R-1 至 R-10。已有 PRD 决定直接继承；下列技术方案成为首期架构基线，原始替代方案和取舍保留作决策依据。完整规则见 [ARCHITECTURE-SPINE.md](ARCHITECTURE-SPINE.md)，技术依据见 [technology-evidence.md](technology-evidence.md)。

| 编号 | 已接受方案 | 实际替代路径与取舍 | 对应 |
| --- | --- | --- | --- |
| R-1 | 六边形模块化单体；一个 Go hubd 服务拥有领域与存储，hub CLI 通过本地协议访问。 | CLI 直连数据库部署少一个 IPC 层，但容易绕过统一生命周期和权限；多服务便于独立扩容，但首期需要处理更多一致性与运维。 | AD-1、AD-2 |
| R-2 | Unix Socket＋内核 UID 身份；root 管理的账户白名单，分别授予 read/manage。首期有 read 的账户可查全部满足归属条件的目标；每请求和输出前复核，撤销无需服务重启。 | loopback TCP 需额外认证；对象级授权更细，但应进入后续权限范围。Socket 组只是连接门槛，CLI 不持数据库/密钥后端权限。 | AD-3 |
| R-3 | 本地 SQLite 保存关系、当前状态和密文；全局一致性门＋事务/唯一约束＋版本条件提交，无秘密与映射缓存。 | PostgreSQL 更适合多节点与较高并发，增加数据库维护；SQLite 首期单节点较简单，但不支持直接横向扩容，慢 KMS 期间查询会延迟变更。 | AD-4、AD-5 |
| R-4 | 生产使用 OpenBao Transit；密钥留在 OpenBao，密文和状态在 SQLite 同一事务提交。生产故障不回退明文。 | OpenBao KV 直接存秘密，需要解决与元数据双写；本地加密只需维护一个应用，但密钥保护/轮换/恢复仍需专门方案。Transit 要独立运维 OpenBao、TLS、AppRole 与恢复材料，取密依赖在线解密。若已有其他 KMS，可在端口边界替换并重做核实。 | AD-6 |
| R-5 | 凭据可用性用独立字段组合；后台自动刷新，取密不即时访问 TikTok。App/安装共享 refresh resource，兑换和刷新共用 App 级协调；row_revision CAS、独立秘密 generation，持久化待处置结果。 | 查询即时刷新把平台延迟和轮换冲突加入取密；直接失败重试无法证明不会重复轮换。平台成功后本地失败也不能盲目重放；须按契约复核或受控重新授权。 | AD-7 至 AD-9 |
| R-6 | 公开回调与本地管理分独立 listener/mux；会话、随机 state、App 绑定、持久化幂等；provider profile 统一平台能力和秘密用途。 | 共享路由容易误暴露取密；通用 OAuth 模板不能替代 TikTok Seller 核实。Q-7 依然是相关接入 Story 的前置条件。 | AD-10、AD-11 |
| R-7 | 单独 audit.sqlite，含入站拒绝记录；manage 账户通过 CLI 检索。业务结果确定后尽力审计，失败安全告警、健康降级；journald 与受控系统事件采集，至少一年保留及备份。 | 同业务事务的审计会在故障时阻断原操作，违反 PRD；仅写 journal 的查询与保留更依赖系统配置。未到达服务的连接拒绝须另行系统采集，独立审计不能保证整盘故障无缺口。 | AD-12 |
| R-8 | 共享 contract/v1；identity/credentials/status 三组 JSON、稳定错误码；stdin 单 JSON 对象、显式隐藏交互；命令树和完整 schema 在 CLI Story 前固化。 | 每命令自定格式会产生脚本兼容冲突；秘密参数易进入进程列表或历史。JSON、0/1 退出码、管理秘密排除与整体失败都继承 PRD。 | AD-13 |
| R-9 | Ubuntu Server 24.04 LTS 验收，linux/amd64 和 arm64 构建、归档＋systemd；固定非 root 服务账户、受控 credential；一致备份，恢复后暂停取密/刷新直至复核。 | 沿用现有 Linux/KMS 更省运维，尚未获得目标环境；首期容器/集群不是必需。版本是已接受的初始基线，不等于已部署。 | AD-14、Stack |
| R-10 | 已接受容量/演练目标：10 并发本地请求、KMS 正常时取密 p95 ≤2s；每日一致备份 RPO ≤24h、人工恢复 RTO ≤4h。 | 数值为已确认的首期设计与验收目标，须由实现和恢复演练检验；长期平台轮换风险或基础设施限制要求调整时，显式修订指标与方案，不能承诺已达标。 | Deferred／Q-9 |

本次接受仍保留 Story 前的 CLI、平台接入及部署契约要求；R-4 是技术选择，实际第三方资源修改与部署仍须按具体任务授权执行。

确认来源：本聊天用户“全部采用”，记录于 [.memlog.md](.memlog.md)。原推荐状态已由本次决定取代，不需重复确认。

## Party审查后的采用细化（2026-10-08）

用户要求按审查建议处理全部问题，授权补齐 [状态证据与恢复资格](credential-state.md)、[响应兼容扩展](response-compatibility.md)、[待处置结果保护交接](pending-results.md)。这些是R-5/R-8等已接受原则的具体契约，保留R与AD编号；完整编码和实际验证按更新后的Story/SQ门槛交付，不把修订当作运行验收。
