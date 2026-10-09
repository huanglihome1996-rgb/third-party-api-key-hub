# Epic 2 Context: 运维可完成 TikTok 店铺授权并持续维护可用凭据

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

2026-10-09 用户明确确认目标市场为美国（US）、自家店铺使用的应用；后续平台契约以 TikTok Shop US Seller 自用场景为范围，具体接口与控制台权限按官方依据核实。

延续本地主机凭据核心，完成 TikTok Seller 授权、安全回调、受控异常恢复、可选验证和自动刷新；正确更新当前安装凭据，保留运维停用决定，并以真实平台联调证据证明接入能力。

## Stories

- Story 2.1: 核实目标 Seller 授权、轮换与验证平台契约
- Story 2.2: 细化并验证仅回调的 HTTPS 部署边界
- Story 2.3: 运维可创建并检查安全的授权会话
- Story 2.4: 安全处理授权回调并原子更新当前安装凭据
- Story 2.5: 运维可受控处置平台结果不确定的操作
- Story 2.6: 运维可执行平台支持的凭据验证并区分失效
- Story 2.7: 服务可协调自动刷新并保留本地停用决定
- Story 2.8: 取得目标 Seller 环境的授权和维护联调证据

## Requirements & Constraints

- 实施前核实目标市场、Seller App 类型/权限、官方版本、授权/state/来源校验、店铺身份与 shop_cipher、秘密用途与必需组合、期限/轮换、共享刷新资源和失败分类。Creator/Affiliate 或通用 OAuth 资料不能自动视为 Seller 契约；未知能力不得启用真实授权或刷新。
- 取密和管理仅限服务 Linux 主机的获授权账户；每次请求及提交/明文输出前复核资格。公开 HTTPS 入口只承接回调及无秘密健康检查，无管理 TCP 路由。
- 同 App/店铺重授权只更新当前安装，不提供旧授权版本选择；同平台跨 App 店铺归并。多店铺授权不自动创建品牌映射；缺链或任一必需凭据不可用时，组合取密整体失败。
- 回调、平台原始响应、授权码、state、秘密和密文包不得进入日志、审计、告警、错误或联调报告。操作成功与失败均记录非敏感审计；审计故障限时尽力处理并告警，不回滚业务。
- 虚构适配器测试与真实 Seller 联调分别记录；未核实或未验证项保持阻塞。真实授权/刷新、平台配置和生产部署须有相应外部动作授权；不扩展到 Web 管理、远程取密或 Vaultwarden 推送。

## Technical Decisions

- 用例统一拥有领域变更；适配器仅返回规范化结果和类型化失败。平台网络调用不持领域一致性门；返回后在写门和同一 SQLite 事务中复核权限、profile、epoch、App/resource/依赖 row_revision 快照。
- 授权兑换、重新授权、刷新及处置共用 App 排他协调。远端 grant/refresh secret 共享的安装引用同一 refresh_resource_id；调度持久化 next_attempt_at，取密不触发即时刷新。只对已证明无不确定副作用的失败有界退避。
- state 至少 256 bit 随机，仅持久化哈希、App、发起账户、期限和 authorization_epoch；新会话递增 App epoch，旧 epoch 禁止兑换。领取平台调用前持久化唯一 operation 与快照，授权码不落盘。
- 当前密文绑定 purpose=current-credential、中心目标、type、generation。完整多店铺规范化结果先保护为 pending-provider-result，认证绑定 operation/App/种类/session/epoch/profile/依赖快照，再原子保存。条件提交重新保护为正确当前密文，禁止用途或跨操作互换。
- 未终结调用、结果不明或远端成功后本地失败进入待处置并阻止该 App 普通自动轮换，重启不重放。已保存完整受保护结果只允许本地条件提交；资格撤销后可安全保管结果，不能代发起者提交变更。
- 实际换值递增 generation 并清空旧确认/验证证据，期限来自新值；暂时验证失败仅记录 incomplete，保留同版本确定证据。明确失效仅在版本匹配时停用。刷新保留 disabled 和已有 recovery_basis，但不能创建恢复资格；停用后合格替换/重授权才产生资格，显式 restore 仍复核当前版本、有效性、完整映射和 manage。
- 生产使用 OpenBao Transit；开发模拟不能证明生产隔离。平台 HTTP 10s、KMS 5s、审计 500ms 均服从上层预算，敏感请求禁自动重试/重定向；时钟不可信时停止期限相关查询与调度。

## UX & Interaction Patterns

运维通过本机 CLI 发起授权，进入平台页面同意，再用 CLI 检查非敏感会话/操作状态；生成链接不表示授权完成。沿用 contract/v1 JSON、stdout 仅 JSON、诊断 stderr、成功/失败退出码 0/1。回调仅返回安全结果，不授予取密资格；不确定操作和本地停用由 manage 显式处置/恢复。

## Cross-Story Dependencies

先完成目标 provider profile 和回调隔离验证，再实施会话；兑换同时交付安全阻塞，受控处置先于验证与刷新，真实联调最后进行。Epic 1 的平台无关本地交付不等于这些前置验收通过。

兑换/处置/刷新必须消费前序 PENDING-RESULT-CONTRACT.md 及真实 OpenBao 两用途/跨 operation/App/epoch/profile/快照交换拒绝、轮换旧包解密和重启隔离实证；普通当前值测试不能替代。回调部署必须取得 CALLBACK-EDGE.md、目标 Linux/TLS/证书/日志实测。目标 Seller 输入、完整 CLI schema、结果保护实证与回调部署证据未满足时，对应实施候选不能视为 ready；Epic 2 联调通过也不代替后续性能、备份和恢复验收。
