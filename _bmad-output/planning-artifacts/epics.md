---
status: draft
created: 2026-10-08
updated: 2026-10-08
currentStep: step-04-final-validation
workflowStatus: planning-delivered-with-readiness-gaps
structuralValidation: PASS
implementationReadiness: FAIL
requirementsConfirmed: true
epicStructureApproved: true
epicStructureApprovedOn: 2026-10-08
storiesApproved:
  - '1.1'
  - '1.2'
reviewMode: autonomous
reviewModeSource: 用户于2026-10-08要求后续不逐项确认
implementationReady: false
stepsCompleted:
  - step-01-validate-prerequisites
  - step-02-design-epics
  - step-03-create-stories
inputDocuments:
  - _bmad-output/planning-artifacts/prds/prd-third-party-api-key-hub-2026-10-08/prd.md
  - _bmad-output/planning-artifacts/prds/prd-third-party-api-key-hub-2026-10-08/addendum.md
  - _bmad-output/specs/spec-third-party-api-key-hub/SPEC.md
  - _bmad-output/specs/spec-third-party-api-key-hub/implementation-gates.md
  - _bmad-output/planning-artifacts/architecture/architecture-third-party-api-key-hub-2026-10-08/ARCHITECTURE-SPINE.md
---

# third-party-api-key-hub - Epic Breakdown

## Overview

本文档记录第三方平台 API 凭据管理中心的需求提取结果，供后续 Epic 与 Story 设计使用。首期范围为 Go 模块化单体、Linux 常驻服务 `hubd`、本地 CLI、TikTok Shop 接入、凭据生命周期、完整归属查询、访问边界与审计。

用户已确认输入文档范围、需求清单和三个 Epic 的结构；CE 已形成 34 条规划草案并完成结构/覆盖检查，开发就绪门槛尚未通过。需求摘要不替代输入文件中的逐项验收；后续须共同读取 [PRD](prds/prd-third-party-api-key-hub-2026-10-08/prd.md)、[技术补充](prds/prd-third-party-api-key-hub-2026-10-08/addendum.md)、[SPEC](../specs/spec-third-party-api-key-hub/SPEC.md)、[实施门槛](../specs/spec-third-party-api-key-hub/implementation-gates.md) 和 [架构契约](architecture/architecture-third-party-api-key-hub-2026-10-08/ARCHITECTURE-SPINE.md) 及其 companions。早期 `docs/project-brief.md` 不作为当前基线。

用户随后要求不再逐项确认，由代理继续完成拆分与校验。`approved-for-planning` 仅表示用户明确确认范围；后续 `planned` 表示按该授权自主形成的规划条目，均不表示任务已执行。`planned-gated` 是实施候选草案，不能派发或作为已定稿的可实施 Story：必须先完成所列前序任务和 SQ 证据，再以实际契约细化字段/配置并复核验收。技术门槛继续按上游执行，产品和架构决定不变。

## Requirements Inventory

### Functional Requirements

- FR-1 平台与 App 登记：通过 CLI 登记平台和平台 App；平台、本中心 App 标识、平台 App 标识为必填，唯一性冲突整笔拒绝，并按平台能力校验配置。
- FR-2 安装授权关联：维护平台 App 与外部店铺的多对多安装授权；每份授权独立追踪凭据状态；重复授权更新当前关联并保留历史，不产生正常查询可选的旧版本。
- FR-3 外部店铺归属：维护外部店铺到品牌的多对一映射；冲突拒绝，解除或变更立即影响取密，缺少品牌链时不得输出明文。
- FR-4 内部店铺映射：维护品牌与内部店铺的双向一对一映射；唯一性冲突拒绝，映射变更立即生效，完整链缺失时所有查询路径拒绝明文。
- FR-5 店铺授权：CLI 发起 TikTok Shop 授权，平台页面完成同意，服务安全处理回调并保存凭据和关联；回调幂等，来源或关联校验失败不保存，失败授权不误报更新，也不解除本地停用。
- FR-6 手工导入：支持 App 级和店铺级 API Key/Token 导入；归属、类型和值必填；同类型覆盖必须显式替换；支持隐藏交互和 stdin，任何输入值不得回显或进入结果、日志、审计、告警、错误；未知有效期可在运维确认后取密并明确标记。
- FR-7 凭据保存与验证：独立保存 App 和安装凭据的状态、有效期和验证结果；区分运维确认、平台验证、验证未完成和明确失效；暂时验证故障不得覆盖原可用性，明确失效可停用。
- FR-8 凭据刷新与停用恢复：自动刷新支持刷新的凭据；刷新失败不延长有效期；有效且未停用的旧值可继续返回；本地停用立即拒绝取密；刷新、验证和映射恢复不能自动解除停用；重新授权或明确替换后仍需显式恢复。
- FR-9 按业务归属查询：按品牌、内部店铺或外部店铺取得完整业务凭据；唯一 App/店铺可自动选择，多项时必须显式选择；完整归属、权限和所有必需凭据均满足才返回，任一必需项失败则整体无明文。
- FR-10 JSON 明文结果：CLI 使用稳定机器可读的 contract/v1 JSON；成功包含 identity、credentials、status 和完整归属及状态信息；失败仅返回 error/message，不含秘密；stdout 只输出 JSON，诊断在 stderr，退出码为成功 0、失败 1，同主版本保持兼容。
- FR-11 查询范围与访问控制：仅服务所在 Linux 主机获授权账户可通过 Unix Socket 取密；read 资格允许取密，manage 资格允许变更；未授权账户、其他主机和公开回调不得获得对象信息或取密权限；资格每请求重新复核。
- FR-12 基础访问审计：记录凭据操作、App 登记、映射、入口拒绝等成功和失败事件；审计含时间、Linux 账户、操作、非敏感目标、结果、错误类别和操作 ID，至少保留一年；审计写入失败不阻断原操作并触发安全告警，检索受 manage 限制。

### NonFunctional Requirements

- NFR-1 凭据保密：真实凭据不得进入源码、Git、日志、测试数据、审计记录、系统告警或错误响应；授权查询的明文仅在完整成功结果中返回。
- NFR-2 关系完整性：外部店铺、品牌、内部店铺和安装授权满足唯一性与完整映射约束；冲突整笔拒绝；映射变更后旧关系和缓存不得继续取密。
- NFR-3 回调可靠性：授权回调安全、幂等地保存凭据并更新关联；重试和失败恢复按已核实的平台契约实施。
- NFR-4 平台扩展性：新增平台适配器不修改核心外部店铺、品牌、内部店铺和安装授权关系模型。
- NFR-5 脚本兼容性：标准输出只提供 JSON；成功退出码 0、失败退出码 1；同主版本保持既有字段、类型和含义稳定，诊断与提示使用 stderr。
- NFR-6 可部署性：提供可复现 Linux 构建、打包和 systemd 配置；生产支持专用密钥管理服务。

### Additional Requirements

- 采用 Go 六边形模块化单体；单个 `hubd` 进程拥有领域规则和唯一变更入口，`hub` 只负责 CLI、契约、安全输出和 Unix Socket 客户端职责。
- 使用共同 `contract/v1` DTO、错误码和操作定义；未知操作、重复/未知 JSON 字段和超限请求拒绝；所有操作带服务生成的 operation ID。
- 领域关系、凭据状态、映射和授权会话由 application 用例统一变更；SQLite 事务、读写一致性门、row revision CAS、秘密 generation 和 authorization epoch 遵守架构约定。
- 唯一性由数据库索引、外键和用例校验共同保证：平台 App、安装授权、外部店铺身份、品牌/内部店铺映射以及同目标同类型当前凭据均不得产生重复当前记录。
- 凭据值只以密文和元数据存储；生产使用 OpenBao Transit，SQLite 不保存明文，不做跨请求秘密缓存；密文绑定不得混用目标、用途、版本或权限；当前值与待处置平台结果的两用途规则按架构pending-results.md。
- 可用性由有效期、平台验证、运维确认、本地停用、凭据值版本等正交字段计算；停用、验证、恢复和映射变化不能隐式推进或恢复错误状态。
- 刷新、回调和外部平台调用不得持有领域写门；网络完成后必须以版本快照和 CAS 检查结果，外部副作用不确定时进入恢复路径，不盲目重放。
- 平台适配器只返回规范化结果和类型化失败；TikTok Seller 市场/App 类型、授权协议、店铺身份、Token 期限/轮换、验证能力和失败分类必须形成 provider profile 与官方依据后才能实施对应 Story。
- 本地管理协议使用路径型 Unix stream socket `/run/api-key-hub/hub.sock`，通过 `SO_PEERCRED` 和 root 所有的 UID/账户白名单授权；无管理 TCP listener，公开入口只承接受控回调。
- 审计仓储与业务仓储分离；审计字段不含秘密，失败告警写入 journald/受控采集；系统采集不得记录 argv、请求体、URL query 或秘密。
- 采用 Go `cmd/`、`internal/` 布局，主要模块包括 `domain`、`application`、`contract/v1`、local/callback/sqlite/openbao/provider adapters、migrations、deploy/systemd 和 scripts。
- 初始技术基线为 Go 1.27.1、modernc.org/sqlite 1.60.1、modernc.org/libc 1.77.1、golang.org/x/sys 0.48.0、OpenBao 2.7.1、Ubuntu Server 24.04 LTS；systemd 使用目标发行版维护包。Go/模块版本锁定于构建清单与 go.mod/go.sum，现有 Node package.json 不构成服务运行依赖。
- 开发环境可在 Windows 执行纯 Go 领域/契约检查；Unix 身份、权限、TTY、systemd、OpenBao 和交付验收必须在 Linux 目标环境完成，生产与测试密钥实例和存储隔离。
- 运行验收目标为本地 10 并发、健康 KMS 条件下取密 p95 不超过 2 秒、每日一致备份且 RPO 不超过 24 小时、人工恢复 RTO 不超过 4 小时；必须定义测量口径和恢复演练。
- 首期不建设 Web 管理界面、远程取密、钉钉认证、对象级权限、多租户、多节点、其他平台、业务数据同步或 Vaultwarden 自动推送；后续方向须单独设计。
- 实施门槛 SQ-1 至 SQ-5 必须在相关 Story 视为可实施前形成证据：平台契约、CLI schema/输入契约、OpenBao/AppRole 与恢复验证、systemd/TLS/审计采集部署契约、运行指标与恢复演练。

以下细节承接 AD-1 至 AD-14 与 Consistency Conventions，不能留给各 Story 任意选择：

- AD-1／AD-2：第二服务实例因排他运行锁退出；CLI 不直接访问 DB、KMS 或平台刷新；适配器不写领域库，迁移仅处理 schema。
- AD-3：每请求及输出明文前复核原子替换的 root 所有策略；管理变更提交前复核 manage。策略损坏、账户映射改变或身份不可读时拒绝。运行目录 `hubd:hub-clients 0750`、Socket `0660`；客户端无目录写权限，服务无策略写权限。
- AD-4：App 中心 ID 全局唯一，`(platform, provider_app_id)`、`(app_id, external_shop_id)`、`(platform, provider_shop_id)` 分别唯一；application 归并同平台跨 App 店铺，平台 shop ID 和 shop_cipher 不能替代中心外部店铺 ID。
- AD-5：查询持读门直至整份结果及权限复核完成；全部领域变更在写门内同事务提交。变更提交后发起的查询使用新状态；已线性化的响应可能稍后收到。无明文、映射或可用性缓存，平台网络调用不持一致性门；CAS 冲突返回安全 `input_invalid`。
- AD-6：current-credential密文绑定中心目标、类型和generation；pending-provider-result绑定用途/operation/App/epoch/profile/依赖快照，两用途不互换，1.2设计/1.6隔离验证先于2.4/2.5/2.7；解密后验证；KMS 或事务失败保留原记录，依赖故障不当作平台失效。TLS 校验 CA/主机身份，AppRole 最小授权，生产 SecretID 经 systemd credential 文件提供，不使用 unit 字面量、argv 或生产环境变量。
- AD-7：状态转换按架构credential-state.md，恢复须有停用后合格替换/重授权的recovery_basis事务证据及当前expected_generation、有效性、映射和manage；确认/验证绑定值版本，换值不继承旧证据；自动刷新不能创建恢复资格，再次停用清空。已知到期使用绝对时间，未知为 `expiry_known=false` 与 `expires_at=null`，明确失效执行本地停用。
- AD-8：App/shop 必需业务组合全部解密、校验后服务才序列化；CLI 完整读取、验证后一次写 stdout，不流式输出秘密，不提供 App 独立明文查询；refresh/management 专用值不返回，业务必需 App Secret 不能被误排除。
- AD-9：取密不触发即时刷新，后台使用持久化 next_attempt_at；共享远端 grant/refresh secret 的安装引用同一 refresh_resource_id。授权兑换、重新授权和刷新使用 App 级排他协调；新 generation 与依赖当前值同事务更新。外部副作用可能已发生而结果未安全提交时进入 indeterminate；启动发现未终结操作阻止该 App 自动轮换。已安全保存的加密结果仅重试本地条件提交，不再次调用平台；仅对明确安全失败有界退避，受控恢复也经 App 协调。
- AD-10：持久化会话保存至少 256 bit 随机 state 的哈希、App、发起账户、epoch 和期限，不保存明文授权码；兑换及提交核对 epoch/版本，重复已提交回调仅返回安全完成状态。独立回调 mux 只注册回调和无秘密健康入口；多授权店铺不自动建立品牌映射，HTTPS 访问日志不记录 query、授权码或请求/响应体。
- AD-11：平台网络地址来自服务受控配置；未知能力不伪装成功。手工导入不普遍依赖真实授权或可选平台验证，但仍需核实该路径字段用途和必需组合；未核实 profile 不启用真实授权/自动刷新。
- AD-12：独立 audit.sqlite 在业务结果确定后限时尽力写入；记录内核 UID、账户名、actor kind 以及回调/后台已知发起者。audit list 经 manage 分页过滤；身份、权限、schema/体积拒绝不遗漏，客户端自报拒绝明确标记。未到达服务的拒绝由系统采集验证；写入缺口不伪装补写，journald 白名单告警并降级健康状态。日志/审计/错误不直接格式化输入、平台响应、解密对象或密文包。
- AD-13：IPC 为 HTTP/1.1 JSON `/v1/operations`，每连接一请求，最大请求 1 MiB。稳定错误码为 input_invalid、not_found、selection_required、mapping_incomplete、mapping_mismatch、permission_denied、credential_missing、credential_expired、credential_disabled、internal_error。依次判定权限、输入/对象、选择、归属、必需字段、停用、过期、解密/内部故障；无权限不泄露对象。stdin 为严格单个 JSON 对象读至 EOF，缺项失败；仅显式 interactive 且 TTY 可用时隐藏输入，取消后恢复 TTY，秘密不进入命令参数。
- AD-14：发布 linux/amd64 和 linux/arm64 版本化归档及 systemd/config/migration；非 root hubd 运行，业务/审计目录 0700、文件 0600，CLI 账户不可读 DB/bootstrap credential；关闭 core dump、公开调试和 HTTP 内容日志。启动检查策略、schema、后端身份/密钥，schema 不匹配拒绝启动；SQLite rollback journal、synchronous=FULL、每连接外键，不在网络文件系统运行。
- AD-14：备份覆盖一致业务/审计快照、策略/配置版本和独立 OpenBao 密钥版本/恢复材料；不直接复制写入中的 DB。恢复默认关闭取密和调度，复核映射、账户和平台当前凭据，按需重新授权/验证后显式开放，不假设旧 refresh token 可复用。
- 版本/时间/预算：中心标识区分大小写、不静默 trim，外部数字 ID 按字符串；UTC RFC3339，时钟不可信停止依赖期限的取密及调度并告警。generation 仅在成功更新秘密值时递增，row_revision 在领域状态变化时递增且推进 App 聚合修订，authorization_epoch 表达新授权意图。CLI/本地总 deadline 15s，KMS HTTP 5s、平台 HTTP 10s、审计 500ms，受上层预算约束，关闭敏感连接自动重试与重定向。

验收追溯：SM-1 安装独立性、SM-2 映射唯一性、SM-3 授权获取、SM-4 导入与维护、SM-5 业务取密、SM-6 访问与审计、SM-7 Linux 交付、SM-8 扩展与密钥管理，以及全局 SM-C1 无秘密泄漏、SM-C2 无错误归属与越权，均须落实到后续验收。文档定稿/mock 通过不等于真实平台、Linux 或运行指标验收通过。

### UX Design Requirements

首期没有 Web 管理界面或终端 UI 设计契约；CLI 的交互提示、隐藏输入、取消和 JSON 输出属于 CLI 契约与验收要求，由 Story 细化。

### FR Coverage Map

| FR | Epic 覆盖 | 完成范围 |
| --- | --- | --- |
| FR-1 | Epic 1 | 平台/App 登记、配置及唯一性校验。 |
| FR-2 | Epic 1、Epic 2 | E1 手工导入形成并查询独立安装；E2 授权产生/更新同一当前安装。 |
| FR-3 | Epic 1 | 外部店铺到品牌映射及立即生效。 |
| FR-4 | Epic 1 | 品牌/内部店铺双向唯一及完整归属链。 |
| FR-5 | Epic 2 | TikTok 授权发起、平台同意、回调、结果查询及恢复。 |
| FR-6 | Epic 1 | 双输入安全导入、显式替换、未知期限与运维确认。 |
| FR-7 | Epic 1、Epic 2 | E1 保存/状态/运维确认/本地明确失效；E2 已核实的可选平台验证。 |
| FR-8 | Epic 1、Epic 2 | E1 本地停用与替换后的显式恢复；E2 支持能力的自动刷新、重新授权及不确定结果恢复。 |
| FR-9 | Epic 1、Epic 2 | E1 完整手工凭据组合与选择；E2 授权/刷新后的当前值沿用相同查询规则。 |
| FR-10 | Epic 1、Epic 2 | E1 统一契约、JSON/错误/兼容；E2 新授权和维护命令复用同一契约。 |
| FR-11 | Epic 1、Epic 2、Epic 3 | E1 本地 read/manage 与存储边界；E2 公开回调不扩大权限；E3 发布/恢复后的边界复核。 |
| FR-12 | Epic 1、Epic 2、Epic 3 | E1 已交付命令成功/失败审计、受控检索和故障告警；E2 新平台操作审计；E3 一年保留、系统拒绝采集和备份恢复验证。 |

NFR-1/2/4/5 自 Epic 1 生效，Epic 2/3 延续；NFR-3 由 Epic 2 交付。NFR-6 在 Epic 1 提供可运行的 Linux/systemd 与密钥后端基线，在 Epic 3 完成双架构发布及运行/恢复验收。

## Epic List

### Epic 1: 运维可安全导入凭据，业务脚本可按归属取密

运维在受控 Linux 主机登记 App、维护店铺归属、导入 App/店铺凭据、检查状态、标记失效和停用，并在明确替换后显式恢复；业务脚本按品牌或店铺取得完整且归属正确的业务凭据 JSON。交付一个不依赖真实授权和自动刷新的端到端手工管理闭环。

**FRs covered:** FR-1、FR-2（手工关联）、FR-3、FR-4、FR-6、FR-7（本地状态/确认/失效）、FR-8（停用/替换恢复）、FR-9、FR-10、FR-11、FR-12（已交付操作）。

**实现与验收边界：** Go cmd/internal 起始结构在首个实现任务建立；按用例增量形成 schema。本地权限、OpenBao 密文保护、事务一致性、完整结果与审计从本 Epic 就生效，不推迟到其他 Epic。必须能在目标 Linux/systemd 和隔离 OpenBao 上验证；虚构测试凭据不替代真实字段用途/组合契约。覆盖 SM-1、SM-2、SM-4 的本地场景、SM-5、SM-6 的本地场景及 SM-7/8 基线。

**Story 编写前门槛：** 完成此路径适用的 SQ-2、SQ-3、SQ-4，以及 SQ-1 中手工导入所需的类型、scope、purpose 与必需组合；真实 Seller 授权或可选平台验证不作为本 Epic 的普遍前置。未形成上述契约时，只确认 Epic 范围，不把未细化项写成可实施 Story。

**依赖：** 无后续 Epic 依赖；本 Epic 的命令可以独立运行并交付手工管理价值。

### Epic 2: 运维可完成 TikTok 店铺授权并持续维护可用凭据

在 Epic 1 的同一核心和取密入口上，运维通过 CLI 发起 Seller 授权、平台页面同意、回调检查和结果查询获得店铺凭据；服务自动维护已核实支持刷新的凭据，运维执行可选验证并处置不确定平台结果。重新授权/刷新不会破坏当前值、跨 App 归属或本地停用决定。

**FRs covered:** FR-5、FR-2（授权关联/重复授权）、FR-7（可选平台验证）、FR-8（自动维护/重新授权），延续 FR-9、FR-10、FR-11、FR-12。

**实现与验收边界：** Seller provider profile 和官方依据、独立 HTTPS 回调、持久化会话幂等、epoch/CAS、App 级排他协调、共享 refresh resource、外部副作用恢复以及所有新增操作审计共同交付。取密不触发即时刷新，不假设远端 exactly-once；暂时故障、明确失效和 indeterminate 分别处理。覆盖 SM-3、SM-4 平台维护场景及新增入口的 SM-5/6 回归；须有受控真实平台联调证据。

**Story 编写前门槛：** SQ-1 Seller 市场/App 类型、官方版本、轮换/重试/验证契约与联调计划；SQ-2 新命令/结果 schema；SQ-3 外部成功后密文保存与失败恢复；SQ-4 回调 TLS/日志/入口约束。未核实能力不得启用或假装实现。

**依赖：** 仅依赖 Epic 1；回调的安全部署与恢复能力在本 Epic 内形成，不等待 Epic 3 才补齐。

### Epic 3: 运维可验证发布质量、保留审计并从故障中恢复

运维获得可复现的双架构 Linux 交付包与精确操作手册，能够监测无秘密的拒绝/审计降级、保持至少一年审计历史、执行每日一致备份，并在故障后按受控流程复核身份/映射/平台当前值后恢复服务。提供运行目标和恢复目标的实测证据。

**FRs covered:** FR-11、FR-12 的发布、系统采集、长期保留和恢复验证；全量首期功能运行/恢复回归。重点完成 NFR-6、SM-7、SM-8 和 Operational Targets。

**实现与验收边界：** linux/amd64 与 linux/arm64 版本化归档，依赖锁定，目标 systemd 配置；业务/审计一致快照、策略/配置版本和独立 OpenBao 恢复材料；无 argv/query/请求体采集，验证未到达服务的拒绝与告警。完成 10 并发、健康 KMS 下 p95 ≤2s、RPO ≤24h 和人工 RTO ≤4h 的测量/演练。恢复默认关闭取密和调度，显式复核后开放。

**Story 编写前门槛：** SQ-3 密钥版本/AppRole 备份与恢复细化，SQ-4 精确部署/采集/保留/存储契约，SQ-5 测量口径、故障条件与恢复演练步骤；各项门槛仍按上游要求在相关 Story 前满足。

**依赖：** 综合验收使用 Epic 1/2 的已交付能力；不作为前两个 Epic 的权限、加密、审计或平台故障处置能力的补票。

### 已确认结构与实施前提

将登记、映射、导入、状态、查询和本地权限/审计合并到 Epic 1，减少同一核心反复拆分，并尽早提供完整用户价值。以真实平台协议及外部副作用为边界独立 Epic 2，以实际运行/恢复证据为边界独立 Epic 3。所有 Epic 遵守 SM-C1/SM-C2。

Epic 顺序为 1 → 2 → 3；后续 Epic 可依赖已交付能力，前序 Epic 不依赖后序 Epic 才能工作。SQ-1 至 SQ-5 未完成不妨碍本次讨论 Epic 范围，但阻止相关实施 Story 定稿；不以创建 Sprint 状态绕过门槛。当前尚未指定具名负责人或日期。

## Epic 1: 运维可安全导入凭据，业务脚本可按归属取密

交付登记、映射、手工导入、状态维护及完整业务取密闭环；本地身份授权、密文保护和无秘密审计随已交付用例一并生效。

**FRs covered:** FR-1、FR-2（手工关联）、FR-3、FR-4、FR-6、FR-7（本地维护）、FR-8（本地停用/替换恢复）、FR-9、FR-10、FR-11、FR-12。

**NFR / Architecture:** NFR-1、NFR-2、NFR-4、NFR-5、NFR-6；AD-1 至 AD-8、AD-11 至 AD-14、Consistency Conventions、Structural Seed。

**拆分规则：** 按用户能力顺序交付，业务表仅在相应用例需要时创建；每个实现 Story 仅依赖先前已完成内容。首个 Go 实现 Story 建立架构约定的 cmd/internal 起始结构，不引入 Web starter，不一次创建未来用例的全部表。

**当前门槛检查：** 仓库尚无完整 CLI 契约、已核实手工导入字段用途/组合 profile、Transit/AppRole 验证契约或精确 Linux 部署证据。SQ-1 至 SQ-4 的相关部分未满足。因此先编写不依赖这些未定技术选择的契约设计 Story；业务实现 Story 在相应契约形成后编写，不能仅凭占位依赖或 mock 宣称可实施。

### Story 1.1: 明确运维命令与业务脚本的统一 CLI 契约

As a 服务主机上的运维人员和业务脚本维护者,
I want 获得可引用、可校验的命令树、输入 schema 与输出/错误契约,
So that 人工操作与脚本调用采用相同约定，后续实现无需自行发明编码或交互行为。

**Status:** approved-for-planning — 用户已于 2026-10-08 确认范围与验收条件；尚未执行或完成。本 Story 交付契约设计，不交付可运行的管理/取密服务。

**Traceability:** CAP-1 至 CAP-12 / FR-1 至 FR-12 的 CLI 界面约定；重点 FR-6、FR-9、FR-10、FR-11、FR-12；NFR-1、NFR-5；AD-3、AD-7、AD-8、AD-12、AD-13 及 Consistency Conventions；SM-4、SM-5、SM-6 的契约验收准备；SQ-2。

**Dependencies:** 已确认 PRD、SPEC 和架构；不依赖未来实现 Story、真实 TikTok 授权、OpenBao 实例或 Linux 部署。具体 Seller 字段用途与组合由 SQ-1 核实，平台特有验证/刷新参数不可在此臆造。

**Scope / Deliverables:** 在 `_bmad-output/planning-artifacts/contracts/cli-v1/` 形成 `CLI-CONTRACT.md`、明确版本与方言的机器可读输入/输出 schemas、按操作分类的虚构合法/非法样例和契约校验结果。覆盖服务 IPC、CLI 参数与 stdin 映射、可用性元数据、无秘密结果和访问资格，保持 `internal/contract/v1` 为未来代码中的唯一契约所有者。本 Story 不实现数据库、平台调用、KMS 或实际 TTY/Unix 身份认证。

**Acceptance Criteria:**

**Given** 已确认的 FR-1 至 FR-12 与 AD-13,
**When** CLI 设计者定稿命令树和操作清单,
**Then** 清单覆盖非敏感本地运行状态、平台/App 登记、映射建立/查询/变更/解除、安装与状态查询、导入/确认/失效标记/停用/显式恢复、授权发起/结果查询、可选验证、维护结果查询及 `audit list`,
**And** 每项记录命令到内部 operation 的映射、输入来源、必填/可选字段、结果类型以及 read/manage 资格；后台自动刷新不要求 CLI 常驻；不增设远程取密、独立 App 明文查询或平台自动撤销接口。

**Given** 命令树和架构已确定的标识、时间与 IPC 约定,
**When** 为每个操作编写 schema 和输入规则,
**Then** 文档与 schema 明确字段类型、缺值/空值、长度/合法字符、大小写、未知/重复 JSON 字段、未知操作、1 MiB 请求体限制及 HTTP/1.1 `/v1/operations` 单请求连接行为,
**And** 稳定 ID 不从名称生成、不静默 trim，外部数字 ID 按字符串处理，时间使用 UTC RFC3339；重复字段、EOF 和体积等 schema 无法独立表达的规则列入解析/协议校验清单，不伪称仅凭 JSON schema 已验证。

**Given** 用虚构 App/店铺类型描述的完整业务凭据组合,
**When** 定义取密成功输出与三种归属查询的选择规则,
**Then** 输出为 `identity`、`credentials`、`status` 三组，identity 具有平台、App、外部店铺、品牌、内部店铺稳定标识，credentials 的 app/shop type/value 与 status 的同类型正整数generation、状态及期限一一对应,
**And** 未知期限为 `expiry_known=false` 和 `expires_at=null`；多 App/店铺要求选择、唯一项自动解析；失败整体无任何部分秘密；所需业务 App Secret 可以返回，refresh/management 专用值不返回；实际类型/必需组合引用 SQ-1 profile，虚构类型仅证明编码。

**Given** 同一请求可能同时无权限、输入错误或存在凭据异常,
**When** 编写错误 schema、判定优先级和合法/非法样例,
**Then** 所有失败只有 `error` 与安全 `message`，固定码覆盖 `input_invalid`、`not_found`、`selection_required`、`mapping_incomplete`、`mapping_mismatch`、`permission_denied`、`credential_missing`、`credential_expired`、`credential_disabled`、`internal_error`,
**And** 优先判定身份/权限，再输入/对象、选择、归属、必需字段、停用、过期、解密/内部故障；无权限不泄露对象信息，依赖故障映射 internal_error；所有命令 stdout 仅该次 JSON、诊断在 stderr、成功 0/失败 1。

**Given** 运维以 stdin 或显式 interactive 导入虚构秘密,
**When** 定义输入、取消和失败处理契约,
**Then** stdin 为一个严格 JSON 对象读至 EOF，缺项/非法数据直接失败且不转交互；只有显式 interactive 且 TTY 可用时才隐藏读取，规定非 TTY、EOF、中断、超时后的结果与 TTY 恢复行为,
**And** 秘密不出现在 argv、提示、导入结果、诊断、审计或错误，已存在同类型凭据须显式替换；字段用途未知、有效期未知和刷新能力未知分别处理；不将未知期限解释为永不过期。

**Given** 停用后的凭据或缺失归属链，以及可分页检索的非敏感审计,
**When** 定义显式恢复和审计查询输入/输出,
**Then** 恢复入口明确目标、运维意图与失败原因，要求重新授权或明确替换之后的有效当前值、完整映射和 manage；刷新成功/验证成功/映射恢复不能隐式解除停用,
**And** 审计查询定义时间、操作、目标、结果、operation_id 过滤和确定的分页语义，要求 manage，记录仅含非敏感白名单字段；客户端不通过输入账户/角色字段取得资格。

**Given** 已确定的 15s 本地总 deadline、错误码及主版本兼容规则,
**When** 设计服务响应验证、一次输出和取消/连接故障场景,
**Then** 文档明确 CLI 必须完整读取并验证服务结果后才输出业务秘密，截断、畸形或失败结果无部分秘密输出；不重试或重放未证明安全的操作，取消或连接丢失不代表服务业务已回滚,
**And** 同主版本既有字段/类型/含义保持稳定，可选新增与破坏性调整有明确兼容示例；契约不能改变上游权限、期限、整体失败或停用恢复规则。

**Given** 命令树、schema、虚构样例与边界校验清单均已形成,
**When** 逐操作校验文档/样例与 PRD、AD-13 的一致性,
**Then** 所有首期 CLI 操作均有输入、非敏感结果/安全错误和合法/非法样例，机器可校验部分通过且失败样例按预期被拒绝，未确定的公共字段/输入规则不存在占位决定,
**And** 报告区分已完成的静态契约校验、SQ-1 尚待核实的平台类型规则，以及未来 Linux TTY/身份/部署和真实平台的运行验收；完成本 Story 不能宣称后者或整个 SQ-1 至 SQ-5 已通过。
**Given** 业务使用的版本可能早于服务当前值,
**When** 定义mark-invalid、confirm、restore和主动disable输入,
**Then** 前三者必须携带目标/type/expected_generation，版本冲突为安全input_invalid且不变更新值；disable明确表示主动停用当前目标,
**And** 查询每type状态返回同快照generation，秘密值不作比对材料；命令/样例引用credential-state.md。

**Given** 旧CLI与同主版本新服务交换结果,
**When** 在response-compatibility.md指定状态元数据位置增加虚构可选字段,
**Then** 旧CLI成功验证已知字段并重建输出、不透传未知字段；未知秘密/重复项/错误响应夹带值仍失败,
**And** 请求未知字段仍拒绝，既有类型/含义不变；逐操作非敏感扩展位置必须在schema中明确。

**Given** 本地管理请求提交后CLI超时或取消且审计降级,
**When** 定义调用者确认结果的流程,
**Then** 通过目标的非敏感当前generation/row_revision和管理结果查询复核当前状态，不依赖审计作为唯一提交证据、不盲目重放变更,
**And** 无法证明本次提交结论时明确报告结果不确定；写stdout开始后的I/O故障不追加第二JSON、不宣称服务回滚。


### Story 1.2: 明确当前凭据与待处置结果的密文保护契约

As a 负责凭据安全存储的维护人员,
I want 获得统一的密文编码、归属绑定、解密校验和失败处理契约,
So that 后续导入与取密不会因交换密文、版本混用或依赖故障泄露秘密或破坏当前凭据。

**Status:** approved-for-planning — 用户已于 2026-10-08 确认范围与验收条件；尚未执行或完成。交付密钥保护设计与可复核的验证步骤，尚未实现 SecretProtector，也不表示 OpenBao 实际验收通过。

**Traceability:** CAP-6、CAP-7、CAP-9 / FR-6、FR-7、FR-9；NFR-1、NFR-2、NFR-6；AD-2、AD-5、AD-6、AD-8、AD-14、Consistency Conventions；SM-4、SM-5、SM-8、SM-C1、SM-C2 的存储验收准备；SQ-3 中 Transit 绑定、交换拒绝和旧密文解密部分。

**Dependencies:** 已采用的 OpenBao 2.7.1、AD-6 与版本约定；不依赖 Story 1.1 的执行结果、未来业务实现、TikTok 授权或部署 Story。设计和验证步骤引用 OpenBao 官方依据。需要实际隔离后端才能取得的验证证据明确保持待执行，不将未来环境存在作为本设计 Story 的完成条件。

**Scope / Deliverables:** 在 `_bmad-output/planning-artifacts/contracts/secret-protection/` 形成 `SECRET-PROTECTION-CONTRACT.md`、`PENDING-RESULT-CONTRACT.md`、带明确版本的密文包/绑定编码说明、`Seal/Open` 端口输入/结果/类型化失败约定、虚构合法/非法样例和验证矩阵。文档固定目标、类型、generation 绑定与数据库提交顺序，给出可在隔离 OpenBao 上执行的交换/轮换/故障验证步骤及预期结果。本 Story 不实现业务用例，不配置生产 OpenBao、不发放或变更真实凭据；AppRole 身份生命周期及完整部署/备份恢复另行细化。

**Acceptance Criteria:**

**Given** AD-6 要求密文绑定中心目标、凭据类型和秘密值版本,
**When** 定义密文包、绑定编码与 SecretProtector 端口,
**Then** 明确 App/安装目标的无歧义标识、type、generation、包格式版本、Transit 密钥引用/版本的来源与校验规则，以及待加密数据和待验证绑定的边界,
**And** 实现者可依完整字段/类型/编码示例构造并校验密文包；generation 初值为 1 且只随成功更新秘密值递增，current-credential绑定不加入row_revision/authorization_epoch；pending-provider-result认证绑定领取快照/适用epoch，仍复核当前CAS；元数据不能独自证明解密结果的归属。

**Given** 一份有效密文和另一个目标、类型或 generation 的期望绑定,
**When** 设计解密后绑定复核与交换验证用例,
**Then** 合法绑定可取得精确虚构原值，而跨 App、跨安装、跨 type、跨 generation 的交换均必须拒绝，不能输出被交换秘密或部分业务组合,
**And** 对受损密文、篡改包、缺字段、未知格式版本和无效密钥引用给出拒绝步骤与安全失败结果，错误/诊断只取非敏感白名单字段，不格式化明文、密文包或后端响应。

**Given** application 准备手工导入或显式替换当前凭据,
**When** 规定加密与 SQLite 写事务的顺序,
**Then** 先完成密文保护，之后在一致性写门内按领域校验/CAS 将密文、generation 与当前元数据原子提交；无显式替换或发生加密/事务失败时不覆盖原记录、不产生第二份当前值,
**And** 加密成功而事务未提交不等于导入成功，不将明文写入磁盘以等待重试；停用与值版本更新分开，不能因重新加密或导入成功自动解除停用。

**Given** OpenBao 不可用、TLS 校验失败、请求超时或解密绑定失败,
**When** 定义需加解密操作的失败行为和预算,
**Then** 取密及需加解密的变更整体失败，无明文降级或跨请求秘密缓存，当前业务调用成功结果没有部分凭据；依赖故障安全映射为 contract/v1 的 internal_error,
**And** KMS/DB 故障不冒充平台失效、不延长凭据期限、不解除停用；密钥 HTTP 5s 预算服从上层 15s deadline，校验 CA/主机身份，敏感连接关闭自动重试与重定向。

**Given** 平台授权或刷新可能已在远端生效而本地保护/提交失败,
**When** 在文档中区分本地导入失败与外部副作用不确定,
**Then** 明确前者遵守原记录不变，后者交由 AD-9 的 indeterminate/受控恢复流程，不能据本地事务失败宣称远端回滚或旧 Token 必然仍有效,
**And** 已安全保存的加密结果只允许重试本地条件提交，未安全保存的结果不能转明文落盘，不能通过 SecretProtector 失败重放未经证明安全的平台请求；本 Story 固定 pending-results.md 所要求的结果包编码、用途/operation/App/epoch/profile/依赖绑定和状态交接，不实现平台恢复业务。

**Given** 已持久化引用旧 Transit 密钥版本的密文和独立隔离验证后端,
**When** 编写密钥轮换后旧密文解密及错误密钥恢复的验证步骤,
**Then** 步骤明确已知虚构样本、当前/旧密钥版本、期望绑定、轮换前后比较和故障注入结果，验证旧密文可解且换错密钥/缺失必要版本时安全拒绝,
**And** 验证方案不导出生产密钥、不把密钥或真实秘密写入业务库/测试资料，保留旧密钥版本与独立恢复材料的要求交接到 AppRole/备份恢复设计；轮换密钥不等于更新业务秘密 generation。

**Given** 编码、端口、提交顺序和验证矩阵已完整记录,
**When** 对照 AD-6、AD-14 与 SQ-3 核对本设计 Story 的完成证据,
**Then** 设计可按虚构样例复核且没有待实现者自行选择的绑定/包格式/错误处理占位，官方依据有版本和适用性记录，所有验证步骤有条件与预期结果,
**And** 报告分别标识静态设计已完成与隔离 OpenBao 实测待执行；没有实际验证结果时不得标记交换拒绝、旧密文解密、完整 SQ-3 或生产保护已通过，AppRole 生命周期、部署和恢复演练继续保持未完成。
**Given** 尚未提交领域当前值的虚构多店铺平台结果,
**When** 编写PENDING-RESULT-CONTRACT.md的完整包/schema/编码,
**Then** 绑定格式版本/purpose/operation_id/App/种类/session-epoch/profile/已知依赖快照，完整规范化秘密在包内；不提前猜中心目标或最终generation,
**And** 与current-credential用途严格隔离，解密核对期望绑定而非仅信元数据；引用pending-results.md，不写授权码/原始body/真实秘密。

**Given** 平台已返回结果但manage撤销、CAS/epoch/profile过时或进程崩溃,
**When** 固定结果保护、原子保存和本地条件提交矩阵,
**Then** 明确protected-result/indeterminate/terminal转换、App阻塞和重启保护，完整包与operation原子保存；仅全匹配时重新保护为正确当前值并同事务提交,
**And** 条件不匹配仍保留待处置证据，失败不转明文落盘、不重放平台；给出暂存包保留/安全清理步骤和1.6隔离验证预期。


### Story 1.3: 明确 OpenBao 服务身份的发放、续期与回收契约

As a 维护服务身份的运维人员,
I want 一套最小授权的 AppRole 和 credential 生命周期操作约定,
So that 服务可在身份到期、轮换和回收时安全失败或受控恢复。

**Status:** planned — 仅规划，尚未执行；完成产物是设计契约，不能代替后端或运行验收。

**Traceability:** NFR-1、NFR-6；AD-6、AD-14；SM-8、SM-C1；SQ-3。

**Dependencies:** 无前序 Story 依赖，仅采用上游约定；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-3 的 AppRole 生命周期设计；本条本身用于形成设计证据。

**Scope / Deliverables:** contracts/secret-protection/IDENTITY-LIFECYCLE.md，权限白名单、生命周期状态表、虚构配置及故障验证步骤；不实际发放生产身份。

**Acceptance Criteria:**

**Given** OpenBao 2.7.1 与 SecretProtector 职责已采用,
**When** 核实官方 AppRole、Transit 和 token 语义并设计最小授权,
**Then** 记录角色/密钥命名、必要 API 路径与能力、身份期限/续期条件及 SecretID 发放限制，应用身份不获得密钥导出或管理权限,
**And** 注明依据版本/适用性；CA、地址、角色和策略由服务受控配置确定，不来自客户端。

**Given** 生产 bootstrap SecretID 需注入非 root hubd,
**When** 定义发放、交付、替换及回收流程,
**Then** SecretID 使用 systemd credential 文件，受控账户不能读取；unit、argv、生产环境变量、日志及示例均无真实身份材料,
**And** 规定原子交付、文件权限、轮换验证与旧身份回收顺序；设计不存放真实 SecretID。

**Given** 当前 token 接近到期、已撤销或后端暂时不可达,
**When** 定义续期/重新认证和失败处理,
**Then** 明确何时续期、何时按受控 SecretID 重新认证、何时停止加解密并告警，身份失效不触发明文降级,
**And** 给出超时/权限拒绝/轮换失败分类与可验证结果，避免无界重试或反复登录。

**Given** 服务身份与独立 OpenBao 恢复身份分属不同权限边界,
**When** 编写隔离验证和操作交接清单,
**Then** 覆盖成功认证、最小权限拒绝、续期、重新认证、SecretID/token 回收及回收后的失败行为,
**And** 设计与未来实测分开；恢复材料不与业务密文共库，完整恢复目标交由后续恢复任务。

### Story 1.4: 明确受控 Linux 主机的运行、权限和审计采集契约

As a 维护服务主机的运维人员,
I want 可核验的账户、systemd、Socket、策略及无秘密告警配置,
So that 只有受控本地账户能管理和取密，拒绝与审计故障可被发现。

**Status:** planned — 仅规划，尚未执行；完成产物是设计契约，不能代替后端或运行验收。

**Traceability:** FR-11、FR-12；NFR-1、NFR-6；AD-3、AD-12、AD-14；SM-6、SM-7；SQ-4。

**Dependencies:** 1.3；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-4 的本地运行/权限/采集设计；本条形成契约，实测由 1.7 取得。

**Scope / Deliverables:** contracts/linux-runtime/LOCAL-RUNTIME.md，systemd/账户/策略/采集的虚构配置、指令支持性依据与隔离验证步骤；公开回调留在 Epic 2。

**Acceptance Criteria:**

**Given** 目标为 Ubuntu 24.04 LTS 与发行版 systemd,
**When** 记录精确 unit 指令和账户路径,
**Then** 固定非 root hubd、RuntimeDirectory hubd:hub-clients 0750、Socket 0660、业务/审计目录 0700 与文件 0600，root 所有策略在服务只读的独立位置,
**And** CLI 无 DB/bootstrap credential 读权限和运行目录写权限；禁用 core dump、公开调试和 HTTP 内容日志，核实指令支持性。

**Given** SO_PEERCRED 与 UID/账户白名单决定 read/manage,
**When** 细化原子策略替换及账户撤销规程,
**Then** 每请求和返回明文前重新读策略，管理提交前复核 manage；策略损坏、身份无法读取、账户名变化或 UID 复用未更新均拒绝,
**And** Socket 组成员身份不自动赋权；部署脚本不允许 hubd 改写策略，不开放管理 TCP。

**Given** 拒绝可能发生在请求到达服务之前,
**When** 选择本地系统采集通道并定义事件字段,
**Then** 可收集连接拒绝的非敏感身份/时间/端点信息，服务内拒绝、客户端自报与系统事件来源可区分,
**And** 不采集 argv、请求体、URL query、秘密或原始平台响应；对象未知保持未知，不伪报服务已审计。

**Given** 审计写入可能失败且历史至少保留一年,
**When** 设计 journald 健康降级、告警和保留规则,
**Then** 明确结构化白名单告警、采集规则与演练触发条件，原操作不因审计失败回滚；记录存储、保留边界与定期备份接口,
**And** 本任务输出确定配置和预期结果，实际 Linux 证据留待 1.7；不宣称设计文档等于部署验收。

### Story 1.5: 核实 TikTok 手工导入所需字段用途与业务凭据组合

As a 负责 TikTok 接入的设计者,
I want 针对目标市场和 App 类型的手工导入能力 profile,
So that 导入和查询按真实字段用途工作且不依赖未核实授权流程。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-1、FR-2、FR-6、FR-7、FR-9；AD-4、AD-8、AD-11；SM-1、SM-4、SM-5；SQ-1。

**Dependencies:** 无前序 Story 依赖，仅采用上游约定；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** 需要目标市场/App 类型的可靠输入和对应官方文档访问；SQ-1 手工路径子集。

**Scope / Deliverables:** contracts/provider/tiktok-manual-profile.md，类型/scope/purpose/required 表、身份归并规则、官方依据与能力支持表。

**Acceptance Criteria:**

**Given** 用户或受控平台材料给出目标市场与 Seller App 类型,
**When** 核对对应官方版本及适用性,
**Then** 记录平台 App/shop 标识、shop_cipher 的上下文与用途、App/安装凭据类型、business/management 用途及必需组合,
**And** 不从 Creator、Affiliate 或通用 OAuth 推定 Seller 事实；缺少目标信息时记录真实阻塞，不编造 profile。

**Given** 运维从 stdin 手工导入凭据而不做真实授权,
**When** 定义每类输入及类型校验,
**Then** 必需归属/类型/值明确，有效期与刷新字段按能力区分必填/可选/不支持；未知期限经运维确认可用,
**And** 可选平台验证不是普遍前置；未知刷新能力不宣称可自动刷新，缺完整映射仍不允许取密。

**Given** 同平台店铺可能关联两个 App,
**When** 定义平台身份到中心店铺的规范化,
**Then** 中心 external_shop_id、平台 provider_shop_id 与按安装上下文的 shop_cipher 分开；同平台跨 App 店铺归并,
**And** 示例仅虚构，至少覆盖两 App/两店铺的归属与不同必需组合，profile 不要求改变核心关系模型。

**Given** 官方证据不足以确认某字段或能力,
**When** 核对 profile 可供下游使用的范围,
**Then** 明确已证实/未证实项与来源，只有已核实手工路径可成为相关实施 Story 的输入,
**And** 真实授权、轮换、验证和联调仍由 Epic 2 核实，不将本子集当作 SQ-1 全部通过。

### Story 1.6: 取得当前值/待处置结果交换拒绝与身份生命周期的隔离证据

As a 负责凭据安全验收的维护人员,
I want 在隔离 OpenBao 上验证密文和身份契约,
So that 后续保存和取密有实证支持而非只依据 mock。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** NFR-1、NFR-6；AD-6、AD-14；SM-8、SM-C1、SM-C2；SQ-3。

**Dependencies:** 1.2、1.3；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** 隔离 OpenBao 2.7.1、受控测试身份和 TLS 环境；取得 SQ-3 手工路径证据后才能编写存储实施 Story。

**Scope / Deliverables:** contracts/secret-protection/validation-report.md、可复跑隔离验证 harness 与去敏结果；同时验证1.2的当前值与待处置结果用途绑定，以虚构多店铺结果测试，不依赖真实平台或未来业务；不修改生产后端。

**Acceptance Criteria:**

**Given** 独立测试实例、身份和虚构秘密按前序契约准备,
**When** 运行 Seal/Open 与交换/损坏矩阵,
**Then** 正确绑定精确解出虚构原值，跨目标/type/generation、错误密钥/格式和损坏数据均拒绝,
**And** 仅 harness 为本任务所需，不实现未来业务表；报告不存 token、密文包、请求体或真实秘密。

**Given** Transit 密钥轮换且保留旧版本,
**When** 复跑旧密文解密与缺失/错误版本场景,
**Then** 旧密文可按原绑定解密，不可恢复所需版本时整体失败,
**And** 密钥轮换不改变业务 generation，独立恢复材料的存在性和访问边界可核验而不导出生产材料。

**Given** 最小授权 AppRole、可续期 token 和轮换/回收流程已定义,
**When** 执行成功认证、续期、重新认证和回收验证,
**Then** 身份可按契约生命周期工作且越权 API 被拒绝；回收后不能继续加解密,
**And** CA/主机错误、超时和后端不可用均安全失败，无明文降级或自动重放。

**Given** 隔离验证全部运行或环境存在真实阻塞,
**When** 整理结果与证据链接,
**Then** 每项有环境版本、步骤、预期/实际结果与失败原因，实际通过项才标通过,
**And** 缺环境或失败保持门槛未满足；不因验证 harness 存在而声称生产保护已验收。
**Given** 1.2提供当前值与待处置包编码，隔离后端和虚构多店铺结果可用,
**When** 运行用途/跨operation/App/epoch/profile/依赖快照交换及篡改矩阵,
**Then** 正确绑定可精确解出虚构完整结果，所有用途互换或错误绑定拒绝且无部分秘密,
**And** 报告将结果保护子项与当前密文子项分别列出；harness不要求真实平台、未来业务表或生产资源。

**Given** 保护结果已保存、模拟保存失败或密钥轮换后重启,
**When** 复跑独立harness的结果状态/旧包解密/本地条件提交检查,
**Then** 完整保存才成为protected-result，未安全保存按indeterminate拒绝重放；旧包可按原绑定解密，条件改变安全拒绝,
**And** 本子项通过证据是2.4/2.5/2.7定稿前明确Gate，不代替这些任务自己的业务崩溃/撤权恢复验收。


### Story 1.7: 取得 Linux 身份、权限和入口拒绝采集的隔离证据

As a 负责主机安全验收的运维人员,
I want 在目标 Linux 上验证本地权限及采集契约,
So that 非授权账户不能绕过入口并且拒绝不会悄然丢失。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-11、FR-12；NFR-1、NFR-6；AD-3、AD-12、AD-14；SM-6、SM-7；SQ-4。

**Dependencies:** 1.3、1.4；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** 受控 Ubuntu 24.04 LTS 测试主机、systemd 与独立测试账户；取得 SQ-4 本地子集证据。

**Scope / Deliverables:** contracts/linux-runtime/validation-report.md 与独立 Socket/身份/策略/采集探针，不需要未来生产服务。

**Acceptance Criteria:**

**Given** 只使用隔离账户和独立临时路径的身份探针,
**When** 在 systemd 下创建路径型 Socket 并执行授权/拒绝矩阵,
**Then** 核验 SO_PEERCRED 身份、目录/Socket/DB/credential 权限及 root 只读策略；获组访问但无白名单资格仍拒绝,
**And** 探针独立可完成，不依赖未来 hubd；不触及生产账户和策略。

**Given** 请求期间原子替换白名单、破坏策略或改变账户映射,
**When** 复核每请求与输出前的拒绝结果,
**Then** 撤销按契约生效，身份损坏/无法读取安全拒绝，客户端账户/角色不能覆盖内核身份,
**And** 未授权查询不透露对象，另一个主机无可达的管理入口。

**Given** 连接被系统权限拒绝而服务探针未收到请求,
**When** 执行系统采集与告警演练,
**Then** 可检索真实非敏感拒绝事件并与服务内拒绝区分，采集可证明未到达服务的事件来源,
**And** 扫描采集/日志确认没有 argv、请求体、query 或秘密，不伪报业务 operation。

**Given** 所有隔离权限/采集和 systemd 指令检查完成,
**When** 保存可复跑证据,
**Then** 记录发行版、systemd 版本、实际结果与未满足项,
**And** 只有实际通过才满足相应 SQ-4 子集；不宣称业务服务、公开回调或完整生产部署已通过。

### Story 1.8: 运维可启动受控本地服务并检查安全运行状态

As a 服务主机运维人员,
I want 通过受控 CLI 检查单实例服务的非敏感运行状态,
So that 确认服务和权限边界正常后再登记或管理业务凭据。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-10、FR-11、FR-12；NFR-1、NFR-5、NFR-6；AD-1、AD-3、AD-12、AD-13、AD-14；SM-6、SM-7。

**Dependencies:** 1.1、1.3、1.4、1.6、1.7；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-2 本地协议/状态命令、SQ-3 后端身份/密钥检查与隔离实证、SQ-4 本地权限实证；本条正式编写前细化状态 DTO 与配置。

**Scope / Deliverables:** 首个生产 Go cmd/hub、cmd/hubd 与必要 internal 模块、锁定 go.mod/go.sum、最小 systemd；仅创建当前入口审计所需 schema。

**Acceptance Criteria:**

**Given** 前序本地契约和 Linux 权限证据可引用,
**When** 建立 Go 1.27.1 cmd/internal 起始结构并运行 hubd/hub,
**Then** 通过 Unix HTTP/1.1 单请求连接访问无秘密状态命令，非 root hubd 运行且第二实例因运行锁失败退出,
**And** 无 Web starter/管理 TCP；状态只说明真实已实现能力，不创建未来业务表或伪装未来命令成功。

**Given** root 所有 UID/账户策略与内核身份可读取,
**When** 执行授权、客户端伪造、策略撤销和损坏矩阵,
**Then** 每请求及响应前重新复核资格；状态操作按契约赋权，未知/无权限操作以安全 JSON 拒绝,
**And** Socket 权限不是业务资格；CLI 不能读 DB/引导身份，策略不可由 hubd 改写。

**Given** 入站操作成功或被身份/权限/schema/体积拒绝,
**When** 记录当前入口审计并返回结果,
**Then** 用独立 audit.sqlite 记录 UTC、内核 UID/账户、actor kind、非敏感目标、结果、错误类别、operation_id，未知目标不拷贝输入,
**And** 审计限时 500ms、失败产生白名单告警/健康降级且不改变原结果；业务 stdout 无额外文本。

**Given** 畸形/重复字段、超限体、超时、schema 不匹配或无法读取策略,
**When** 执行契约与启动故障验证,
**Then** 严格拒绝输入，启动检查策略、当前schema及后端身份/密钥，必要检查失败拒绝启动并给无秘密诊断；CLI stdout JSON 和 0/1 退出码、15s 总 deadline 符合契约,
**And** Linux 身份/systemd 在目标环境验证，Windows 领域/协议测试不替代它。

### Story 1.9: 运维可检索非敏感审计并发现审计降级

As a 获 manage 资格的运维人员,
I want 按目标和操作 ID 分页检索成功/失败记录,
So that 追踪管理操作及入口拒绝并及时发现审计缺口。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-10、FR-11、FR-12；NFR-1、NFR-5；AD-3、AD-12、AD-13；SM-6。

**Dependencies:** 1.1、1.4、1.7、1.8；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-2 审计 schema/分页、SQ-4 采集/保留/告警契约。

**Scope / Deliverables:** audit list CLI/用例、统一审计接口与受限客户端拒绝上报；沿用 1.8 独立审计库。

**Acceptance Criteria:**

**Given** manage 调用者访问 audit list,
**When** 按时间/操作/目标/结果/operation_id 过滤分页,
**Then** 只返回符合白名单的事件，分页遵循前序确定语义；read-only/未授权账户拒绝且不透露记录,
**And** 源身份来自内核，后台/回调 actor kind 与已知发起者字段可扩展而不伪造账户。

**Given** CLI 在服务业务用例前拒绝非法输入,
**When** 尽力提交受限枚举 client-rejection,
**Then** 服务以内核身份记录为客户端自报，消息和目标不含原始输入或秘密,
**And** 上报失败保留原 CLI 错误并有安全诊断；系统拒绝依前序采集，不伪报服务已收到。

**Given** 审计数据库不可写或连接超时,
**When** 运行非敏感操作并注入故障,
**Then** 原结果照常返回，journald 告警且健康降级，可检索历史只反映实际已写入事件,
**And** 不伪造缺口补写或无丢失承诺；至少一年保留规则不因失败降级缩短。

**Given** 后续每个业务用例需要审计,
**When** 验证当前统一接口与无秘密 DTO,
**Then** 成功/失败接口可携 operation_id、非敏感目标和错误类别使用，避免直接格式化平台 body/解密对象/密文包,
**And** 已有命令的 stdout 仍单 JSON；只测试当前能力，不引用未来命令证明覆盖。

### Story 1.10: 运维可登记并查询唯一的平台 App

As a 集成运维人员,
I want 从 CLI 登记平台和平台 App 并检查当前记录,
So that 为后续店铺凭据提供唯一可辨认的 App 归属。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-1、FR-10、FR-11、FR-12；NFR-1、NFR-2、NFR-4；AD-2、AD-4、AD-5、AD-11；SM-1。

**Dependencies:** 1.1、1.5、1.8、1.9；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-1 登记配置/字段用途、SQ-2 登记 schema、SQ-4 主机边界。

**Scope / Deliverables:** registry CLI/用例与当前 Platform/App schema、唯一索引/外键；秘密字段仍经后续导入入口，不在 argv 登记。

**Acceptance Criteria:**

**Given** 获 manage 资格并输入已核实平台注册类型,
**When** 登记平台、中心 App ID、provider_app_id 和可选显示名,
**Then** 有效 App 可被安全状态查询定位；中心 App ID 全局唯一，platform/provider_app_id 组合唯一,
**And** 显示名不当身份，不静默 trim；缺少所需配置安全反馈，不假设各平台一致。

**Given** 缺字段、重复 ID、无权限或并发重复登记,
**When** 调用同一 application 用例,
**Then** 整笔拒绝且原记录不变，写门内同事务提交、提交前复核 manage,
**And** 每连接启用外键、rollback journal/FULL 及 schema 版本；只迁移本 Story 所需业务实体。

**Given** App 记录变更和登记失败,
**When** 查询操作结果/审计,
**Then** operation_id、row_revision 与审计符合统一约定，适配器不写数据库,
**And** 不提供 App 独立明文取密；未知/越权对象无秘密也不泄露不允许的信息。

### Story 1.11: 运维可维护外部店铺与品牌归属

As a 集成运维人员,
I want 登记稳定店铺身份并建立或变更其品牌归属,
So that 业务脚本可按正确品牌定位店铺且冲突不会抢占映射。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-3、FR-10、FR-11、FR-12；NFR-2、NFR-4；AD-2、AD-4、AD-5；SM-2。

**Dependencies:** 1.1、1.5、1.8、1.9、1.10；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-1 店铺身份规范化、SQ-2 映射 schema、SQ-4 本地边界。

**Scope / Deliverables:** 外部店铺/品牌登记及映射用例和所需实体/index；不提前建内部店铺或凭据表。

**Acceptance Criteria:**

**Given** 虚构同平台 shop ID 从两个 App 上下文输入,
**When** 登记中心外部店铺并维护品牌关系,
**Then** platform/provider_shop_id 唯一且归并到同一中心 external_shop_id，品牌可关联多个店铺,
**And** shop_cipher 不当中心 ID；名称不改变稳定身份，App 上下文不误建第二份店铺。

**Given** 店铺已属品牌或输入与既有映射冲突,
**When** 显式变更/解除映射或并发创建冲突关系,
**Then** 成功变更在写门同事务提交；唯一性冲突整笔拒绝、原状态不变，不自动抢占,
**And** 映射解除可保留未来凭据，当前查询归属读到提交后新状态，不缓存旧映射。

**Given** manage 被撤销或目标不可识别,
**When** 提交映射变更并检查审计,
**Then** 提交前资格复核，失败无越权对象信息；审计有非敏感目标/operation_id,
**And** 仅使用前序能力证明映射查询和原子性，未来业务取密验收在对应查询 Story 复测。

### Story 1.12: 运维可维护品牌与内部店铺的一对一映射

As a 集成运维人员,
I want 从品牌或内部店铺两端维护唯一映射,
So that 内部业务身份与外部店铺归属一致且缺链可被明确识别。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-4、FR-10、FR-11、FR-12；NFR-2；AD-2、AD-4、AD-5；SM-2。

**Dependencies:** 1.1、1.8、1.9、1.11；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-2 完整映射 schema、SQ-4 权限。

**Scope / Deliverables:** 内部店铺实体与双向唯一索引、映射用例/CLI，完整归属链判定。

**Acceptance Criteria:**

**Given** 品牌与内部店铺各有稳定 ID,
**When** 建立、查询、变更或解除映射,
**Then** 两端定位同一关系，双向一对一唯一性由用例/索引保证；冲突整笔拒绝不改变原链,
**And** 显示名不代替身份；manage 提交前复核、每次变更推进相关 row_revision 并审计。

**Given** 完整外部店铺→品牌→内部店铺链已建立,
**When** 解除任一段后重新解析归属,
**Then** 所有归属解析路径报告缺链，恢复另一完整链后旧身份不再匹配,
**And** 不缓存旧关系，写门提交后新请求使用新链；恢复映射不包含任何凭据启用副作用。

**Given** 两个并发请求竞争同一品牌或内部店铺,
**When** 检查事务结果和失败 JSON,
**Then** 只能形成合法唯一映射，失败为契约安全错误且保留旧记录,
**And** 仅创建本 Story 所需内部店铺/映射 schema；缺链无明文不依赖未来实现才能判定。

### Story 1.13: 运维可从标准输入安全导入 App 级凭据

As a 获 manage 资格的运维人员,
I want 以严格 stdin JSON 导入或显式替换 App 级秘密,
So that 业务 App 调用凭据可受保护保存且不经命令参数暴露。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-6、FR-7、FR-10、FR-11、FR-12；NFR-1、NFR-2；AD-2、AD-4、AD-5、AD-6、AD-7、AD-13；SM-4。

**Dependencies:** 1.1、1.2、1.3、1.5、1.6、1.8、1.9、1.10；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-1 类型/用途、SQ-2 stdin schema、SQ-3 绑定/AppRole实证、SQ-4存储权限。

**Scope / Deliverables:** App current credential schema/SecretProtector适配器/导入用例；当前值版本和停用字段从首次存储正确建立。

**Acceptance Criteria:**

**Given** 已登记 App 且 profile 明确类型与 scope,
**When** 从 stdin 单个 JSON 至 EOF 导入,
**Then** 归属、type、value 缺任一均失败；有效期/刷新字段按 profile 校验，未知期限明确标记，不推定可刷新,
**And** 不进入交互，不使用秘密 argv，不输出导入值，管理响应只有非敏感元数据。

**Given** 同 App 同 type 已有当前值,
**When** 未显式替换、显式替换成功或故障后查询当前状态,
**Then** 未显式替换整笔拒绝；成功替换原子推进 generation/row_revision 与 App 聚合修订；失败原记录不变,
**And** 初值 generation=1；disabled/disabled_generation 不被替换解除；同目标/type 不产生第二当前值。

**Given** KMS 可用或被注入超时、错误绑定、DB提交失败,
**When** Seal 后按当前 expected generation 提交,
**Then** SQLite 仅存密文/版本引用/元数据，加密与元数据原子保存，无明文磁盘/缓存降级,
**And** TLS和5s预算遵守契约，故障不标平台失效，成功失败都有无秘密审计。

**Given** 读者只有 read 或 App 不存在/错误scope,
**When** 尝试导入,
**Then** 按权限/输入/对象优先级安全拒绝，不披露未获权限对象,
**And** 正式产物仅实现当前 App 导入路径，不创建未来安装或回调实体。
**Given** 当前虚构值A已确认或已验证且明确替换为B,
**When** 原子保存B及其当前状态,
**Then** 按credential-state.md清空A的confirmed_generation/validation证据，采用B本次期限；无新确认的手工B不能仅凭A证据可用,
**And** 同次显式确认B绑定其新generation；若此前disabled则保存本次替换的recovery_basis_generation/operation但不解除停用，状态与密文同事务。


### Story 1.14: 运维可安全导入店铺凭据并形成独立安装关联

As a 获 manage 资格的运维人员,
I want 把店铺级凭据绑定到指定 App/外部店铺组合,
So that 两个 App 的店铺授权和当前值可独立维护而不混用。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-2、FR-6、FR-7、FR-10、FR-12；NFR-1、NFR-2；AD-2、AD-4、AD-5、AD-6、AD-7；SM-1、SM-4。

**Dependencies:** 1.1、1.5、1.6、1.9、1.10、1.11、1.13；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-1安装/类型组合、SQ-2店铺导入schema、SQ-3密文实证。

**Scope / Deliverables:** Installation/shop current credential实体与唯一索引、手工导入流程。

**Acceptance Criteria:**

**Given** profile 与已登记 App/中心外部店铺明确,
**When** 导入店铺 scope 秘密,
**Then** 绑定 app_id/external_shop_id 唯一安装，缺归属或type/value失败不保存无归属记录；秘密和安装同事务保存,
**And** 关联创建来自确认手工导入，不要求真实授权；缺品牌/内部店铺映射可保存但标记不满足取密。

**Given** 至少两 App 与两店铺及同组合已存在,
**When** 逐组合导入和显式替换,
**Then** 多对多关联独立可查，同组合只更新当前凭据而不新增候选安装/旧授权版本,
**And** 不同 App 的元数据/密文不混用；shop_cipher 按profile与App/安装上下文处理。

**Given** 同组合存在当前 type 且提交失败或manage撤销,
**When** 执行替换并复核,
**Then** 无明确替换拒绝；成功推进秘密/对象/App版本；失败整笔保留原安装/当前值,
**And** 本地停用保持，失败诊断/审计不回传秘密，KMS失败不当平台失效。
**Given** 当前虚构值A已确认或已验证且明确替换为B,
**When** 原子保存B及其当前状态,
**Then** 按credential-state.md清空A的confirmed_generation/validation证据，采用B本次期限；无新确认的手工B不能仅凭A证据可用,
**And** 同次显式确认B绑定其新generation；若此前disabled则保存本次替换的recovery_basis_generation/operation但不解除停用，状态与密文同事务。


### Story 1.15: 运维可检查状态并确认未知有效期凭据可用

As a 集成运维人员,
I want 独立检查 App/安装凭据状态并记录运维确认,
So that 未知期限不被误认永不过期，业务取密有明确可用依据。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-2、FR-7、FR-10、FR-12；NFR-1、NFR-5；AD-2、AD-5、AD-7；SM-1、SM-4。

**Dependencies:** 1.1、1.5、1.9、1.13、1.14；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-1 状态/期限用途、SQ-2 状态/确认 DTO。

**Scope / Deliverables:** 无明文凭据状态查询及运维确认用例，正交可用性计算。

**Acceptance Criteria:**

**Given** App 或安装凭据有已知/未知有效期,
**When** 查询状态及执行明确运维确认,
**Then** 输出非敏感type/generation/状态/期限；确认必须携expected_generation匹配，confirmed_generation绑定当前值，未知为null和expiry_known=false；确认与平台验证分别记录,
**And** 确认推进row_revision/App聚合修订但不推进generation；不伪称平台验证成功。

**Given** 已知 expires_at<=now、disabled 或未获可用确认的凭据,
**When** 计算当前可用性,
**Then** 过期/停用拒绝可用；未知期限经明确运维确认且其他条件满足才可用,
**And** 不存互相矛盾ready缓存；完整映射和权限另行复核，确认不解除停用。

**Given** 服务时钟不可相信或两个管理请求并发,
**When** 查询或提交确认,
**Then** 停止依赖期限的取密/调度并安全告警，CAS冲突不覆盖状态,
**And** 状态/审计错误无秘密；当前版本检查可用fixture完成，不依赖未来平台验证实现。
**Given** 值A已确认后被替换为B，或运维确认意图指向旧版本,
**When** 检查状态并提交expected_generation确认,
**Then** B不继承A证据；过时确认拒绝不改当前状态，匹配版本确认仅建立该版本证据,
**And** validation确定证据与last_validation_attempt分别表达，未知期限每次换值重新确认；平台获取来源不伪称另行验证成功。


### Story 1.16: 业务脚本可按完整归属取得全部必需业务凭据

As a 获 read 资格的业务脚本维护者,
I want 按品牌、内部店铺或外部店铺一次取得正确 App/店铺的完整 JSON,
So that 无需自行拼接或重复查询且不会使用错误归属秘密。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-9、FR-10、FR-11、FR-12；NFR-1、NFR-2、NFR-5；AD-3、AD-4、AD-5、AD-6、AD-7、AD-8；SM-2、SM-5、SM-6。

**Dependencies:** 1.1、1.2、1.5、1.6、1.9、1.12、1.13、1.14、1.15；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-1必需组合、SQ-2完整结果schema、SQ-3解密/交换实证、SQ-4本地权限。

**Scope / Deliverables:** 统一query用例、三种归属解析/选择、安全完整输出；不实现即时平台刷新。

**Acceptance Criteria:**

**Given** 两App/多店铺或唯一匹配的完整链数据,
**When** 分别按品牌/内部店铺/外部店铺查询,
**Then** 唯一App/店铺自动解析，多项要求明确选择，指定对象须同时属请求范围和所选App,
**And** 无匹配、缺链、不匹配分别安全错误；不返回跨App/店铺秘密列表或App独立明文。

**Given** profile要求的App业务凭据与shopToken均可用,
**When** 持读门读取/解密/复核完整结果,
**Then** identity/credentials/status符合v1，每个type状态含同快照正整数generation；精确返回虚构完整值，包括业务必需AppSecret，排除refresh/management值,
**And** 全部解密/绑定/有效性/权限复核后服务序列化；CLI完整读取验证后一次写stdout，不流式输出。

**Given** 任一必需项缺失、disabled、过期、不可用、错绑定或KMS失败,
**When** 执行组合查询并捕获stdout/stderr/审计,
**Then** 整体失败仅error/message与退出码1，仍可用部分不输出，诊断/审计无秘密,
**And** 错误优先级按AD-13；不因取密触发刷新、不延长期限或缓存旧结果。

**Given** 查询与映射变更/账户撤销并发,
**When** 按线性化点检查结果,
**Then** 写事务完成后发起的新请求使用新关系；返回秘密前再读策略，所有校验在读门内完成,
**And** 已线性化响应可能稍后收到；不将网络接收时刻误作撤销保证，失败无部分秘密。

**Given** 服务返回截断/畸形、违反允许扩展位置的字段、未知秘密或超时,
**When** CLI验证结果与预算,
**Then** 在秘密输出前安全拒绝无效服务结果；允许位置的可选非敏感元数据兼容接受，CLI按已验证字段重建输出、不透传未知字段，stdout单JSON、stderr诊断、成功0/失败1,
**And** 同主版本兼容检查通过；已知状态fixture只测当前能力，不调用未来平台功能。
**Given** 同主版本hubd在允许状态元数据位置新增虚构可选字段,
**When** 旧CLI读取完整业务结果,
**Then** 已知字段/类型/generation与凭据组合验证通过，按已验证字段重建单JSON输出、未知元数据不透传,
**And** 非扩展位置新增字段、未知秘密type、重复字段/条目和错误对象夹带秘密均在业务秘密输出前失败。


### Story 1.17: 运维可通过隐藏 TTY 输入导入凭据

As a 交互运维人员,
I want 显式选择隐藏交互输入 App/店铺秘密并安全取消,
So that 人工导入不暴露秘密且取消不会破坏终端或保存半份数据。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-6、FR-10、FR-12；NFR-1、NFR-5；AD-13；SM-4。

**Dependencies:** 1.1、1.13、1.14；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-2 交互/取消契约和可用Linux TTY环境。

**Scope / Deliverables:** 复用现有导入用例的CLI TTY adapter；非交互流程不变。

**Acceptance Criteria:**

**Given** 显式interactive且TTY可用,
**When** 隐藏读取虚构App/shop秘密后提交,
**Then** 终端不回显，提示在stderr，stdout只安全操作JSON，不通过argv或日志存值,
**And** 复用stdin导入的schema、显式替换、事务和审计，不另造服务操作。

**Given** 非TTY、缺必填内容、EOF、SIGINT/取消或超时,
**When** 运行输入流程,
**Then** 依契约退出并恢复TTY状态，不回传输入、不提交半份秘密，不从stdin缺项自动转交互,
**And** stdin脚本读至EOF失败立即返回；提示不污染JSON，秘密不出现在诊断。

**Given** TTY前序验证条件成立,
**When** 在Linux实际检查回显/取消与成功失败结果,
**Then** 取得操作、退出码和终端恢复证据，样例仅虚构,
**And** Windows/mock不替代TTY验收；当前两类导入均通过契约回归。

### Story 1.18: 运维可停用失效凭据并在显式替换后恢复取密

As a 获 manage 资格的运维人员,
I want 本地标记失效/停用并显式确认新当前值恢复,
So that 业务发现无效凭据时立即停止取密且不会被后台状态变更悄然启用。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-7、FR-8、FR-9、FR-10、FR-12；NFR-1、NFR-2、NFR-4；AD-2、AD-5、AD-7；SM-4、SM-5、SM-8。

**Dependencies:** 1.1、1.9、1.13、1.14、1.15、1.16；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-2停用/失效/恢复schema及SQ-3/4的既有证据。

**Scope / Deliverables:** disable/mark-invalid/restore用例与CLI，正交版本/CAS和审计。

**Acceptance Criteria:**

**Given** App或安装凭据当前值可查询,
**When** 停用或明确失效标记成功后再从三种归属查询,
**Then** mark-invalid要求使用时expected_generation匹配，过时报告以input_invalid拒绝且不改变新值；主动disable独立停用当前值；持写门原子记录disabled/disabled_generation、安全原因并清空恢复资格，后续取密整体失败,
**And** 不调用平台撤销/代理业务API、不提供远程失效上报；generation不因状态操作递增。

**Given** 停用后只做运维确认、映射恢复或成功验证/刷新状态模拟,
**When** 尝试取密或未替换的恢复,
**Then** 仍停用；恢复必须有停用后合格替换/重授权的recovery_basis_generation/operation事务证据，不能只比较generation；刷新不得创建资格、可保留已有资格，再次停用清空资格,
**And** 本任务通过已实现替换流程验收；平台行为用领域事件fixture验证约束，不声称真实刷新实现。

**Given** 显式替换产生新当前值且运维明确恢复,
**When** 对expected_generation重核manage、完整映射、当前有效性和持久化恢复资格后提交,
**Then** 全部满足才清除本地停用，过期/缺链/权限撤销/并发CAS冲突拒绝且原状态不变,
**And** 恢复推进状态/App修订不推进generation；结果与审计可追溯但无秘密。

**Given** 手工管理闭环完成,
**When** 在隔离Linux/OpenBao回归两App/两店铺及冲突/缺链/停用矩阵,
**Then** 完成Epic1适用SM-1/2/4/5/6与Linux/KMS基线证据，业务查询只有完整虚构成功值,
**And** 未知期限确认有效、秘密泄漏扫描及同主版本兼容回归通过；用虚构第二profile/adapter验证复用关系模型而不引入第二生产平台，不宣称SM-3真实授权或运行目标完成。
**Given** 停用g5后仅刷新为g6但没有合格替换/重新授权,
**When** 运维对g6显式restore并在重启后重复检查,
**Then** 无recovery_basis证据则拒绝，不能仅因g6大于disabled_generation而恢复,
**And** 刷新fixture和重启状态均保留停用，恢复资格不依赖审计是否写入成功。

**Given** 停用g5后明确替换g6、再刷新g7且当前值已确认可用,
**When** 检查合格替换证据并对g7显式restore，或再次disable后restore,
**Then** 前者其余条件满足时成功；再次停用清空资格，旧替换证据不能复用,
**And** 密钥重新加密不改变业务generation或资格，全部状态证据与当前值事务保存。

**Given** 业务查询g10后当前值已刷新为g11,
**When** manage以expected_generation=g10执行mark-invalid,
**Then** 返回安全input_invalid，g11不被停用；匹配当前g11的失效报告可停用，主动disable仍独立停用当前值,
**And** 审计只含非敏感目标/版本/结果，不传入或回传Token用于比对。


**Epic 1 拆分结果：** 共 18 条规划；先取得契约与隔离证据，再增量交付各用例。未来平台刷新不成为手工闭环的前置条件。实际的实施候选均受 Gates 约束，尚未正式定稿。

## Epic 2: 运维可完成 TikTok 店铺授权并持续维护可用凭据

延续Epic1的同一核心和取密边界，交付平台授权、结果保存、受控异常恢复、可选验证和支持能力的自动刷新；所有平台实现候选须先形成SQ-1至SQ-4的适用证据。

**FRs covered:** FR-5，FR-2/7/8的平台能力及FR-9/10/11/12的延续。NFR-3重点验收，NFR-1/2/4/5持续生效；核心AD-9/10/11并复用AD-3至AD-8、AD-12至AD-14。

### Story 2.1: 核实目标 Seller 授权、轮换与验证平台契约

As a 负责TikTok接入的设计者,
I want 有官方依据的完整Seller provider profile及受控联调计划,
So that 不会用通用OAuth或其他TikTok身份协议替代真实店铺契约。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-1、FR-2、FR-5、FR-7、FR-8、FR-9；NFR-3、NFR-4；AD-9、AD-10、AD-11；SM-3、SM-4；SQ-1。

**Dependencies:** 1.5；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** 明确目标市场/Seller App类型/权限、官方版本与受控联调环境；当前缺少这些证据。

**Scope / Deliverables:** contracts/provider/tiktok-seller-profile.md，官方证据、协议/能力/失败矩阵、受控联调计划；不自行执行真实授权。

**Acceptance Criteria:**

**Given** 已核实手工profile且目标市场/App类型可靠,
**When** 核对Seller授权URL、回调来源与state使用、兑换和授权店铺接口,
**Then** 明确版本、参数来源、店铺identity/shop_cipher、App与安装scope、必需组合和平台来源校验,
**And** 网络地址来自受控服务配置，不接受客户端任意URL；Creator/Affiliate内容只能在证明适用时采用。

**Given** 平台可能支持刷新/可选验证且存在多店铺共享grant,
**When** 核实Token期限、轮换和refresh_resource模型,
**Then** 明确refresh_resource_id归属/共享关系、旧值有效性、刷新提前量/调度及验证支持性,
**And** 未证实能力标未支持/待核实，不伪称成功；不同安装共享远端secret不得复制成独立轮换资源。

**Given** 兑换/刷新请求出现暂时故障、明确失效或不确定副作用,
**When** 建立失败及安全重试分类,
**Then** 记录有界退避条件、不可重放条件、平台复核和受控重新授权方案，覆盖成功后KMS/DB失败及迟到结果,
**And** CAS不等于remote exactly-once；未知平台旧值有效性不按本地DB失败推定。

**Given** 受控真实App可用于联调且权限范围已明确,
**When** 编写一次授权、重复回调、再次授权、刷新/验证和异常演练计划,
**Then** 每场景有非敏感预期、受控操作范围和证据方式，说明需要的用户/平台输入与外部操作授权,
**And** 设计完成不等于联调完成；官方不足或缺目标信息时保持SQ-1未通过。

### Story 2.2: 细化并验证仅回调的 HTTPS 部署边界

As a 负责授权回调部署的运维人员,
I want 确定TLS、证书、路径与无秘密访问日志配置,
So that TikTok回调可到达而远程访问者不能进入管理或取密协议。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-5、FR-11、FR-12；NFR-1、NFR-3；AD-3、AD-10、AD-12、AD-14；SM-3、SM-6；SQ-4。

**Dependencies:** 1.4、1.7、2.1；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** 受控Linux/TLS测试入口、证书来源及平台回调注册要求；SQ-4回调子集。

**Scope / Deliverables:** contracts/linux-runtime/CALLBACK-EDGE.md、精确配置和隔离路由/日志验证报告；不部署生产资源。

**Acceptance Criteria:**

**Given** 目标市场回调URL要求及Linux基线已明确,
**When** 选择并核实TLS终止组件、证书来源和网络配置,
**Then** 记录完整可实施配置，外部只转发回调路径和允许的无秘密健康入口,
**And** Unix管理协议无TCPlistener；OpenBao不经公开入口访问。

**Given** 独立测试回调探针而非未来业务服务接收请求,
**When** 检查未注册路径、管理路径及错误证书请求,
**Then** 只能命中允许路由，管理/取密路径拒绝且身份边界不扩大,
**And** 探针可独立运行，不依赖未来回调业务Story；实际测试结果保存非敏感证据。

**Given** 虚构授权码/state随query进入测试回调,
**When** 检查edge/代理/服务日志、告警与错误,
**Then** query、授权码、请求/响应体和凭据不被采集或回传；source拒绝事件可安全审计,
**And** 未到达服务事件按系统采集渠道区分，失败告警无原始请求。

**Given** 配置和实测条件存在不足,
**When** 整理证据与部署交接,
**Then** 已验证项才标通过，证书/网络/日志支持性未证实保持阻塞,
**And** 不因本计划存在而执行真实平台回调注册、公开生产部署或扩大访问范围。

### Story 2.3: 运维可创建并检查安全的授权会话

As a 获manage资格的运维人员,
I want 用CLI创建有期限的Seller授权会话并查询非敏感状态,
So that 可在正确App上下文准备平台同意流程并识别旧会话。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-5、FR-10、FR-11、FR-12；NFR-1、NFR-3；AD-2、AD-5、AD-9、AD-10、AD-13；SM-3。

**Dependencies:** 1.1、1.8、1.9、1.10、2.1、2.2；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-1授权发起/state语义、SQ-2会话命令schema、SQ-4回调边界。

**Scope / Deliverables:** authorization session实体、会话发起/状态查询CLI和application，独立mux只提供安全拒绝/状态入口；本条不声称已兑换成功。

**Acceptance Criteria:**

**Given** manage账户指定已登记且profile可授权App,
**When** 创建授权会话,
**Then** state至少256bit随机，只存哈希/App/发起账户/authorization_epoch/截止时间；新会话推进App授权意图epoch,
**And** 安全返回profile允许的授权链接和会话引用，日志/审计不存state明文或授权码；平台同意页面仍在平台。

**Given** 同App已有待授权会话,
**When** 创建新会话并查询两份状态,
**Then** 旧epoch不能成为可兑换会话，状态按真实流程报告待授权/过期等非敏感结果,
**And** 浏览器等待阶段不占平台App排他网络操作门，generation与epoch不混用。

**Given** 账户资格被撤销、会话过期或请求畸形,
**When** 发起/查询会话并检查拒绝,
**Then** 当前manage资格按本地契约复核，未授权对象信息不披露，取消不伪称服务已回滚,
**And** 操作均无秘密审计，严格schema/期限/0-1输出复用contract/v1。

**Given** 尚未实现授权兑换,
**When** 验证当前会话管理功能,
**Then** 发起/状态查询独立可验收，未兑换只显示真实未完成状态，不把预生成URL当授权成功,
**And** 仅建本任务会话实体，回调兑换及成功安装更新由后续2.4交付。

### Story 2.4: 安全处理授权回调并原子更新当前安装凭据

As a 完成平台同意的运维人员,
I want 让回调安全且幂等地把Seller结果保存到正确App/店铺,
So that 重复或迟到回调不会覆盖新授权、混用安装或误报完成。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-2、FR-5、FR-7、FR-9、FR-11、FR-12；NFR-1、NFR-2、NFR-3；AD-2、AD-4、AD-5、AD-6、AD-7、AD-9、AD-10、AD-11；SM-1、SM-3、SM-5、SM-6。

**Dependencies:** 1.2、1.6、1.9、1.14、1.15、1.16、1.18、2.1、2.2、2.3；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-1真实兑换/身份/失败profile、SQ-2结果schema、SQ-3结果保护：1.2的PENDING-RESULT-CONTRACT.md和1.6对应隔离实证必须均通过；SQ-4回调配置。当前密文测试通过不替代结果保护子门槛。

**Scope / Deliverables:** provider授权兑换/店铺规范化adapter、回调用例、durable operation/CAS与App排他协调；失败先形成可安全查询的待处置状态。

**Acceptance Criteria:**

**Given** 回调对应合法state哈希、未过期当前epoch和有manage发起者,
**When** 按profile验证来源并领取兑换操作,
**Then** session/operation唯一约束及App级排他协调防重复兑换，领取前持久化operation和App/已知对象快照,
**And** 网络调用不持一致性门，App未知店铺阶段仍按App范围协调；不保存明文授权码。

**Given** 平台返回已核实店铺与业务/管理凭据,
**When** 规范化身份、加密结果并在写门提交,
**Then** 按pending-results.md先保护完整结果，再复核快照并转换为current-credential目标/type/新generation密文；同平台跨App店铺归并，当前安装与密文/状态/generation同事务更新，重新授权只替换同组合当前值；确认/验证证据不继承旧值,
**And** 最终核对epoch/profile/CAS与当前manage，保留disabled，并为实际换值的停用类型保存重授权恢复资格；多授权店铺不自动绑定品牌或内部店铺。

**Given** 回调重复、来源错误、App/目标不匹配、旧epoch、过期或CAS冲突,
**When** 处理请求及查询会话结果,
**Then** 重复已提交只返回安全完成状态；无效/迟到结果不能覆盖，失败不误报当前更新,
**And** query/body/授权码不入日志或响应，所有成功失败无秘密审计；正常取密仍检查完整链。

**Given** 请求可能已生效但KMS/DB失败、manage撤销或崩溃,
**When** 检查durable operation、启动状态和再次轮换资格,
**Then** 进入indeterminate/待处置，阻止该App后续自动轮换；可安全保管加密结果时保存但不替撤权用户提交变更,
**And** 没有安全加密保存的秘密不落盘，不盲重放；本Story即具备停机/拒绝保护，处置操作由2.5扩展。

**Given** 成功后业务查询或失败后状态检查,
**When** 复用已有完整取密/停用规则,
**Then** 只返回当前组合，不提供旧授权候选；重新授权不自动恢复本地停用,
**And** 用虚构adapter完成幂等/崩溃/权限矩阵；真实Seller联调证据单独在2.8取得，mock不冒充它。
**Given** 1.2/1.6结果包子门槛已通过且平台结果包含多个已核实店铺,
**When** 保存暂存结果、模拟崩溃后再本地提交,
**Then** 验证purpose/operation/App/epoch/profile/依赖快照，全部结果保护成功后才标protected-result；匹配才重新保护为正确中心目标/type/新generation并原子提交,
**And** 用途互换/跨操作包/过时条件拒绝，重新授权清空旧确认/验证并建立合格恢复证据，不调用平台第二次、不解除停用。


### Story 2.5: 运维可受控处置平台结果不确定的操作

As a 获manage资格的运维人员,
I want 检查并处置App上未安全终结的授权操作,
So that 不通过盲重放或启用旧值恢复服务且能解除确认已解决的阻塞。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-5、FR-7、FR-8、FR-10、FR-12；NFR-1、NFR-2、NFR-3；AD-5、AD-6、AD-7、AD-9、AD-10；SM-3、SM-4。

**Dependencies:** 1.1、1.9、1.18、2.1、2.4；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-1平台复核/安全重试、SQ-2处置命令、SQ-3加密结果保存：明确消费1.2/1.6的待处置结果设计与隔离实证，以及2.4实际保存/启动保护。

**Scope / Deliverables:** operation检查/显式处置用例，profile允许的平台复核或受控重新授权恢复路径，沿用App协调。

**Acceptance Criteria:**

**Given** operation处于indeterminate或加密结果已取得但未提交,
**When** manage查询操作和阻塞原因,
**Then** 只显示非敏感操作/版本/安全错误信息，未结操作使普通自动轮换继续拒绝,
**And** 启动不会重放未结请求，普通业务可用性只依profile已证实旧值状态判断，不假定仍有效。

**Given** 完整加密结果已安全保存且领域依赖快照仍匹配,
**When** 显式申请仅本地条件提交,
**Then** 验证pending包用途/operation/App/epoch/profile/依赖绑定，再复核权限与快照后可原子提交；转换为正确当前密文，绝不直接复用暂存包，无再次平台调用；条件不满足拒绝且保留待处置证据,
**And** 审计有操作关联，成功处置不会隐式恢复disabled或更改generation约定。

**Given** 结果尚未安全保存或快照不再有效,
**When** 按照profile允许的复核或受控重新授权处理,
**Then** 通过同一App排他协调完成确认，只有明确安全终结才解除阻塞,
**And** 不能把CAS当远端幂等、不能宣称旧refresh可复用或外部已回滚，秘密不转明文落盘。

**Given** 重授权完成且之前本地停用,
**When** 随后执行已有显式restore,
**Then** 必须expected_generation匹配，具停用后合格重新授权的recovery_basis事务证据，并满足当前有效性、完整映射和manage才恢复取密,
**And** 并发处置/撤权/崩溃后仍留durable安全状态，全部动作/拒绝无秘密审计。

### Story 2.6: 运维可执行平台支持的凭据验证并区分失效

As a 集成运维人员,
I want 使用已核实可选验证并记录明确失效或未完成结果,
So that 暂时平台故障不破坏有效凭据也不会恢复过期停用值。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-7、FR-8、FR-9、FR-12；NFR-1；AD-5、AD-7、AD-11；SM-4、SM-5。

**Dependencies:** 1.5、1.9、1.15、1.16、1.18、2.1、2.4、2.5；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-1验证接口/支持范围/明确失效证据、SQ-2验证命令schema。

**Scope / Deliverables:** App/安装可选验证adapter与维护用例、类型化结果归属/CAS。

**Acceptance Criteria:**

**Given** profile明确某App/安装type支持验证,
**When** manage发起验证并持版本快照,
**Then** adapter返回规范化成功/明确失效/未完成且归属当前目标，不写业务库，网络不持领域门,
**And** 未知/不支持能力安全反馈不伪称成功，手工导入无需强制平台验证。

**Given** 平台超时或暂时故障,
**When** 保存验证未完成,
**Then** last_validation_attempt记录incomplete，保留同generation已有确定validation_result/validation_generation和confirmed_generation；不伪装本次成功、不覆盖原可用性、不推进generation、不延长期限,
**And** 已过期/disabled/原不可用状态不因未完成恢复，运维确认与平台验证结果分别表达。

**Given** 返回明确已核实失效证据,
**When** 在写门按快照与权限复核提交,
**Then** 目标本地停用并记录非敏感原因，后续组合取密整体失败,
**And** 迟到验证不能覆盖新generation/CAS快照；审计/诊断无原始平台body或凭据。

**Given** 成功验证或字段模拟变化而目标已disabled,
**When** 检查查询结果,
**Then** 停用仍需已有显式恢复流程；验证结果不能替代完整映射/权限校验,
**And** 只对已核实接口实现，真实平台通过证据留给2.8。
**Given** 当前g有确定valid验证证据，随后本次验证超时；或旧g验证在换值后返回,
**When** 提交验证尝试及检查可用性,
**Then** 同g超时只记last_validation_attempt=incomplete，保留该g既有valid/确认；旧g迟到valid或invalid结果均不得更改新值,
**And** 失效证据匹配当前值才停用并清空恢复资格，验证成功不创建恢复资格、不延长期限。


### Story 2.7: 服务可协调自动刷新并保留本地停用决定

As a 依赖可用凭据的业务脚本维护者,
I want 服务在profile允许时自动更新当前值并安全处理失败,
So that CLI无需常驻且刷新不混用版本、不重复轮换共享资源。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-7、FR-8、FR-9、FR-12；NFR-1、NFR-2；AD-2、AD-5、AD-6、AD-7、AD-9、AD-11；SM-4、SM-5。

**Dependencies:** 1.6、1.9、1.15、1.16、1.18、2.1、2.4、2.5；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-1轮换/共享资源/调度/重试、SQ-3持久化结果：消费1.2/1.6结果包契约与隔离实证、2.4/2.5业务恢复；SQ-2维护状态。

**Scope / Deliverables:** refresh_resource与必要持久化任务schema、next_attempt_at调度、共享App协调与平台refresh adapter。

**Acceptance Criteria:**

**Given** profile证实App或安装可刷新且有规范化refresh_resource,
**When** 后台按已核实参数领取next_attempt_at任务,
**Then** 共享远端grant/secret的安装引用同一resource，不拆成并行轮换任务；领取持久化operation和版本快照,
**And** 兑换/重授权/刷新共用App排他协调至安全终结，取密不触发即时刷新，CLI不常驻也运行。

**Given** 刷新成功且App/resource/依赖对象CAS仍有效,
**When** 加密并原子提交所有依赖当前值,
**Then** 实际换值的generation与row_revision及App聚合修订按约定推进，共享资源相关新值同事务更新；旧确认/验证证据按credential-state.md重置，profile成功获取不伪称独立验证,
**And** 停用决定和已有recovery_basis保持，刷新不能创建/推进恢复资格；状态查询只报实际结果，无跨请求明文缓存。

**Given** 明确无不确定副作用的安全失败、超时或平台失效,
**When** 按profile分类处理,
**Then** 仅安全失败有界退避且不延长期限；旧值有有效证据且未disabled才可返回，过期/停用整体失败,
**And** 未知副作用不当安全重试，KMS/DB错误不自动当平台失效，轮换后旧值不未经证实复用。

**Given** 外部可能已轮换而本地保存失败、撤权、CAS冲突或进程崩溃,
**When** 重启并尝试后续任务,
**Then** indeterminate阻止该App自动轮换，待处置由已有2.5流程处理,
**And** 不可重放未经证明安全的平台调用；加密结果仅本地条件提交，仍本地停用须显式恢复。

**Given** 服务时钟不可相信或不同App有并发维护,
**When** 执行调度与并发故障矩阵,
**Then** 依赖期限任务停止并告警，不同App状态不混用，operation领取本身不推进领域修订,
**And** 10s平台/5sKMS服从上层预算，结果和失败均无秘密审计，未核实profile禁用真实刷新。
**Given** 停用凭据有或没有停用后合格替换/重新授权证据,
**When** 后台实际换值并保存成功结果,
**Then** 均保持disabled，刷新不创建/推进recovery_basis，只保留已有资格；旧确认/验证证据清空且期限来自本次值,
**And** 未换值类型状态不继承其他类型证据，未知期限新值须重新确认；结果包与当前包用途交换拒绝，崩溃后按已有安全状态处置。


### Story 2.8: 取得目标 Seller 环境的授权和维护联调证据

As a 负责平台验收的集成运维人员,
I want 按已核实计划验证真实授权、回调及支持能力,
So that 确认平台接入实际可用而不是仅通过虚构adapter。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-2、FR-5、FR-7、FR-8、FR-9、FR-11、FR-12；NFR-1、NFR-3；SM-1、SM-3、SM-4、SM-5、SM-6、SM-C1、SM-C2。

**Dependencies:** 1.16、1.18、2.1、2.2、2.3、2.4、2.5、2.6、2.7；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** 目标Seller市场/App/权限与受控联调环境；执行真实授权/刷新/第三方变更前需相应外部动作授权。

**Scope / Deliverables:** 非敏感平台联调报告，场景结果与证据引用；真实秘密不入Git/样例/报告。

**Acceptance Criteria:**

**Given** 受控授权范围和真实环境已准备,
**When** 执行CLI发起→平台页面同意→HTTPS回调→CLI状态查询,
**Then** 可定位正确App/店铺/安装并确认当前值完成，重复回调不重复兑换/当前安装，旧epoch和来源错误拒绝,
**And** 取得SM-3真实证据但报告不保存URLquery/授权码/token/body；失败不误报已更新。

**Given** 有重新授权、支持刷新/验证的测试目标,
**When** 执行profile支持的维护矩阵,
**Then** 当前值轮换/期限/共享resource与profile一致，暂时故障/明确失效区分，旧值只依平台证据可用,
**And** 本地停用不自动解除，明确替换/重授权后显式恢复；不支持能力明示，不能伪造通过。

**Given** 权限撤销、缺映射、KMS故障或不确定结果场景可安全演练,
**When** 按受控计划检查业务查询/处置,
**Then** 失败无部分秘密或错误归属，不确定App阻止盲重放且可依计划恢复,
**And** 当前环境无法执行的场景标未验证/阻塞，不利用生产环境做未授权故障注入。

**Given** 实际平台结果与已采用profile不符,
**When** 整理联调结论,
**Then** 记录差异和影响，修改相应profile/契约并回归受影响Stories后才通过,
**And** 文档/mock不替代联调；通过Epic2不等于性能/RPO/RTO或整个生产验收完成。

**Epic 2 拆分结果：** 共8条；平台profile/回调隔离验证在实现之前，会话管理先于兑换，安全阻塞随兑换实现，受控处置先于自动刷新。前序功能无需Epic3才能安全工作。

## Epic 3: 运维可验证发布质量、保留审计并从故障中恢复

交付双架构可复现Linux包、审计保留与采集运营、一致备份、安全恢复和实测运行目标。前序Epic已具备本地权限、加密、审计与不确定副作用阻塞，Epic3进一步形成运维能力和综合验收证据。

**FRs covered:** FR-11/12及全量功能的发布/恢复回归；重点NFR-6、SM-7/8和SQ-3/4/5，SM-C1/C2持续生效。

### Story 3.1: 明确审计保留、一致备份和运行恢复的验收契约

As a 负责运行质量的运维人员,
I want 有精确测量口径与可执行步骤的备份/恢复/保留契约,
So that 发布前可判断运行目标是否真正满足并按同一流程演练。

**Status:** planned — 仅规划，尚未执行；完成产物是设计契约，不能代替后端或运行验收。

**Traceability:** FR-11、FR-12；NFR-1、NFR-6；AD-6、AD-12、AD-14、Operational Targets；SM-6、SM-7、SM-8；SQ-3、SQ-4、SQ-5。

**Dependencies:** 1.2、1.3、1.4、2.1、2.8；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-3独立密钥恢复材料、SQ-4备份存储/保留、SQ-5测量与故障定义；本任务形成设计，不取代实测。

**Scope / Deliverables:** contracts/operations/OPERATIONS-CONTRACT.md，备份manifest/一致性边界、保留规则、恢复状态机、性能/RPO/RTO测量计划。

**Acceptance Criteria:**

**Given** 业务与审计在独立SQLite中且审计允许非阻断缺口,
**When** 确定备份一致性与存储契约,
**Then** 明确两库快照的冻结/排空或等价一致边界、对应策略/配置版本、独立OpenBao密钥版本/恢复材料及恢复可检验manifest,
**And** 不得只拷贝写入中的DB，不把已存在审计缺口伪报已恢复；密钥恢复材料与业务快照独立保护。

**Given** 审计至少一年且要每日一致备份,
**When** 定稿保留/调度/存储/损坏检测步骤,
**Then** 明确一年时间语义、时钟与清理边界、备份可读性/权限、定期执行失败告警和可验证恢复条件,
**And** 不臆造保留存储容量或缩短一年要求；无真实秘密/密钥进入文档、命令或manifest。

**Given** 恢复可能使用过时映射/账户/平台token,
**When** 定义恢复状态机与核对清单,
**Then** 恢复默认关闭取密和调度，先核对映射/账户/平台当前值及必要重新授权/验证，最后由受控运维显式开放,
**And** 缺密钥版本、不可确定refresh状态或身份边界未复核时停在安全状态，不默认旧refresh可复用。

**Given** 目标为10本地并发、健康KMS p95<=2s、RPO<=24h、人工RTO<=4h,
**When** 定义端到端测量范围、样本/百分位方法、健康KMS判据及故障/起止时刻,
**Then** 测量包含实际本地CLI/服务/解密/完整响应路径，RPO与RTO有可复核时间和数据边界,
**And** 不同测量环境、冷启动/维护负载/失败样本处理及恢复材料准备前提明确；不暗中缓存、HA或调整目标。

### Story 3.2: 运维可获取可复现的双架构 Linux 发布包

As a 发布服务的运维人员,
I want 锁定版本的linux/amd64和linux/arm64归档与systemd配置,
So that 可在目标Linux确定地构建安装并检查启动边界。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** NFR-1、NFR-6；AD-1、AD-3、AD-6、AD-14、Stack；SM-7、SM-8。

**Dependencies:** 1.8、1.17、2.2、2.8、3.1；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** 构建工具链/模块可核验、两架构Linux验证环境、SQ-4精确部署契约。

**Scope / Deliverables:** scripts/build/package、构建清单/校验和、版本化归档、systemd/config/migrations与无秘密安装说明。

**Acceptance Criteria:**

**Given** 架构已锁定Go1.27.1/sqlite1.60.1/libc1.77.1/xsys0.48.0/OpenBao2.7.1,
**When** 按构建清单从同一输入重复构建,
**Then** 提供hub/hubd两目标架构归档、版本信息与校验依据，锁定go.mod/go.sum，记录可复现比较结果,
**And** Node基础package不是服务运行依赖；依赖安全核验问题不得静默换已采用版本。

**Given** 目标发行版systemd和前序精确配置可用,
**When** 安装并启动两个目标平台包,
**Then** 非root、Socket/数据/身份权限、运行锁、schema/后端身份检查符合契约,
**And** schema不匹配拒绝启动，公开调试/coredump/内容日志禁用，无管理TCP。

**Given** 安装包或配置误含秘密,
**When** 检查归档、示例与运行参数,
**Then** 无真实密钥/SecretID/token/连接秘密，生产引导使用credential文件，CLI账户不可读存储,
**And** 迁移按当前实际schema版本执行，不为未来无关实体预建表。

**Given** 构建成功但某架构仅交叉编译未运行,
**When** 整理交付验证报告,
**Then** 分别记录两架构构建、安装、运行状态和环境，不用交叉编译代替Linux运行验收,
**And** 现有CLI/TTY/权限回归在可用环境通过，未验证项保持阻塞而非声称完整发布通过。

### Story 3.3: 运维可保留一年审计并检测系统拒绝与告警

As a 负责审计运营的运维人员,
I want 按已确定规则管理审计保留与无秘密告警采集,
So that 长期追踪操作并能发现服务未收到的拒绝和审计丢失风险。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-11、FR-12；NFR-1、NFR-6；AD-3、AD-12、AD-14；SM-6。

**Dependencies:** 1.4、1.7、1.9、2.2、2.8、3.1、3.2；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-4采集/保留/存储配置及测试告警通道。

**Scope / Deliverables:** 保留任务、journald采集/告警配置、可复跑时间边界/入口拒绝/磁盘故障验证。

**Acceptance Criteria:**

**Given** 真实业务审计schema及至少一年保留契约,
**When** 用虚构历史事件测试清理任务,
**Then** 一年内事件不被清理，边界/分页/备份按前序时间语义工作，所有已发布成功失败操作范围被覆盖,
**And** 不要求等待一年才验证策略，报告区分时间fixture测试与长期实际运行证据。

**Given** 系统权限在连接前拒绝或回调edge拒绝,
**When** 执行受控拒绝场景并检索事件,
**Then** 非敏感系统事件可查且来源与服务审计/client-rejection分明，未知目标不伪造,
**And** 采集不含argv/query/body/秘密，不能宣称服务已收到被系统拒绝的业务操作。

**Given** audit不可写、满盘或写入超时,
**When** 触发已实现业务操作,
**Then** 业务结果不回滚，健康降级且白名单告警被实际采集，审计缺口明确可见,
**And** 不承诺同磁盘故障无丢失；诊断不打印平台原文、密文或秘密。

**Given** 无权限用户尝试读取保留历史或采集配置,
**When** 检查权限与audit list,
**Then** manage限制和主机文件权限成立，完整公开回调仍不能进入取密协议,
**And** 事件/告警配置版本可纳入备份manifest，保留策略不因运维方便缩水。

### Story 3.4: 运维可生成每日一致且可核验的完整备份

As a 负责备份的运维人员,
I want 自动生成业务/审计/配置与密钥恢复材料可配套的备份,
So that 故障恢复时不会只有无法解密或状态互相矛盾的数据。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-12；NFR-1、NFR-6；AD-6、AD-12、AD-14；SM-7、SM-8；RPO<=24h。

**Dependencies:** 1.3、1.6、2.5、2.7、3.1、3.2、3.3；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** SQ-3独立密钥备份/恢复契约、SQ-4备份存储/计划任务权限、SQ-5 RPO口径。

**Scope / Deliverables:** 一致快照/manifest/校验与每日调度脚本、无秘密失败告警；独立受控OpenBao恢复材料关联。

**Acceptance Criteria:**

**Given** 业务正在变更且audit独立写入,
**When** 按3.1定义的一致边界生成两库快照,
**Then** 快照、策略/配置版本、schema/构建版本与所需OpenBao密钥版本关联可核验，数据库完整性及manifest校验通过,
**And** 不直接复制运行中DB，不把audit缺口伪装补齐，网络文件系统不作为活动SQLite位置。

**Given** 每日任务成功、错过调度、存储不足或快照损坏,
**When** 执行调度与故障演练,
**Then** 每天生成一致备份并识别不可读/损坏/过期备份；失败有无秘密告警，不替换上一份可用备份为半成品,
**And** 使用RPO测量口径记录最近可恢复状态，不仅凭任务触发就声称满足24h。

**Given** 独立OpenBao恢复材料和业务密文分属权限边界,
**When** 核对备份恢复清单,
**Then** 必要密钥版本与身份/策略恢复步骤可定位且授权边界成立,
**And** manifest不含密钥、SecretID/token或真实明文，业务SQLite/归档仅密文与非敏感元数据。

**Given** 配置/schema在备份期间变化,
**When** 检查快照版本匹配或拒绝结果,
**Then** 只发布配套一致的备份或安全拒绝并保留旧份，恢复输入不会跨版本混拼,
**And** 实际恢复能力由3.5验收，本任务不因文件存在宣称已恢复成功。

### Story 3.5: 运维可在关闭取密和调度的状态下恢复服务

As a 负责故障恢复的运维人员,
I want 依步骤恢复业务/审计/密钥版本后显式复核再开放,
So that 恢复不会把过时身份、映射或平台旧Token重新投入使用。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-7、FR-8、FR-9、FR-11、FR-12；NFR-1、NFR-2、NFR-6；AD-3、AD-6、AD-7、AD-9、AD-14；SM-6、SM-7、SM-8；SQ-3、SQ-4、SQ-5。

**Dependencies:** 1.6、1.16、1.18、2.5、2.8、3.1、3.2、3.3、3.4；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** 隔离恢复主机、配套备份和独立OpenBao恢复身份/材料；必要平台复核或重授权具授权范围。

**Scope / Deliverables:** 恢复脚本/runbook、显式安全启动/开放状态和无秘密复核报告。

**Acceptance Criteria:**

**Given** 隔离环境有已校验备份与配套恢复材料,
**When** 按顺序恢复配置/业务/审计/OpenBao密钥版本并启动,
**Then** 默认不允许取密或后台平台调度，schema/身份/密钥版本不匹配拒绝开放,
**And** 旧密文可按正确绑定解密，缺版本/错误材料安全失败，恢复不泄露明文。

**Given** 恢复快照中的账户、映射或Token可能已经变化,
**When** 执行运维核对清单,
**Then** 复核当前账户策略、完整归属及平台当前值，必要目标验证或受控重新授权；未结operation仍阻止App轮换,
**And** 按credential-state.md复核版本证据/恢复资格；按pending-results.md验证暂存包用途/操作绑定，不从审计缺口补造资格，不推定旧refresh可重用，不盲重放备份中持久化任务，已有停用不自动解除。

**Given** 复核全部满足且manage显式开放,
**When** 启用取密与支持调度并测试,
**Then** 正确当前业务组合可用，越权/缺链/停用/过期仍整体失败，无旧归属取密,
**And** 开放有非敏感审计，独立密钥恢复材料不被复制到业务库。

**Given** 部分复核失败、恢复中断或再次启动,
**When** 检查运行状态与安全告警,
**Then** 继续保持关闭或契约规定的受控状态，不因重启绕过显式开放,
**And** 不伪称全部恢复或审计缺口补齐；本任务验证可恢复，实际RTO量化由3.7演练。

### Story 3.6: 验证十个本地并发下完整取密延迟

As a 依赖脚本响应时间的运维人员,
I want 测得健康KMS条件下端到端取密p95,
So that 确认安全校验与完整输出下仍满足运行目标。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-9、FR-10、FR-11、FR-12；NFR-1、NFR-6；AD-5、AD-6、AD-8、Operational Targets；SM-5、SM-C1、SM-C2；SQ-5。

**Dependencies:** 1.16、2.7、2.8、3.1、3.2、3.3；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** 目标Linux实例、健康隔离KMS、虚构完整组合与3.1测量口径。

**Scope / Deliverables:** 可复跑本地benchmark与非敏感报告，环境/样本/错误/百分位可核验。

**Acceptance Criteria:**

**Given** 测量范围和健康KMS判据已由3.1确定,
**When** 以10个本地并发请求重复完整CLI取密,
**Then** 端到端包含身份、完整映射/状态、解密、结果验证与输出，报告样本量/百分位方法和p95,
**And** 健康条件目标p95<=2s，失败与成功样本分别如实呈现；不排除慢请求来制造通过。

**Given** 服务有映射/权限变更和支持维护负载,
**When** 复核测量中的安全边界,
**Then** 一致性门/权限复核/无跨请求秘密缓存依旧成立，失败无部分凭据,
**And** 不为达标暗中延长期限、跳过KMS、引入HA或复用旧Token。

**Given** 有冷启动、KMS故障或无法达到目标,
**When** 分别记录结果与失败条件,
**Then** 健康主指标与附加故障指标按口径区分，未达标明确失败并指出可核实瓶颈,
**And** 不能把15s deadline解释为达标或静默放宽2s目标；必要修改回到已采用决策流程。

**Given** benchmark需要真实请求结果校验,
**When** 收集延迟与操作引用,
**Then** 仅在请求内验证虚构值，不把秘密/stdout完整响应写benchmark日志或报告,
**And** 测试实例与生产隔离，Windows/mock不代替真实Linux/KMS测量。

### Story 3.7: 演练每日备份的 RPO 和人工恢复 RTO

As a 负责灾难恢复验收的运维人员,
I want 按故障条件量化最近可恢复状态与恢复耗时,
So that 能证明RPO<=24h及人工RTO<=4h而非只拥有恢复脚本。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-11、FR-12；NFR-1、NFR-6；AD-6、AD-9、AD-12、AD-14、Operational Targets；SM-6、SM-7、SM-8；SQ-5。

**Dependencies:** 2.8、3.1、3.3、3.4、3.5；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** 隔离演练主机/配套备份/恢复材料、故障窗口与必要平台复核可用性。

**Scope / Deliverables:** 可复跑恢复演练记录，备份状态时间/故障起点/复核开放时点/遗失边界与结论。

**Acceptance Criteria:**

**Given** 每日一致备份及已确定故障/时间口径,
**When** 制造隔离故障并选择可恢复快照,
**Then** 记录故障时刻与最新完整可恢复状态的差，实际RPO<=24h才通过,
**And** 不把不一致/不可解密快照算可恢复；含审计缺口时明确报告而非宣称零丢失。

**Given** 恢复流程需人工复核账户/映射/平台值,
**When** 计时执行3.5至安全显式开放,
**Then** 按约定起点终点记录包括人工复核的实际耗时，RTO<=4h才通过,
**And** 缺必要恢复材料/平台复核造成延期如实计入适用口径，不能暂停计时制造通过。

**Given** 存在过时refresh、停用和旧账户状态,
**When** 检查恢复前后取密/调度,
**Then** 关闭阶段无秘密输出和轮换，开放后只有经复核的正确当前组合可用,
**And** 系统/服务拒绝和恢复事件可安全检索，秘密与身份材料不进入演练报告。

**Given** 环境无法完成演练或目标未满足,
**When** 保存真实结论,
**Then** 未验证保持门槛未过，失败说明可核实条件和修正入口,
**And** 不暗中改目标/缓存/HA/重用旧refresh，本规划拆分完成不代表运行目标通过。

### Story 3.8: 形成首期 Linux 交付与安全验收证据包

As a 负责首期交付的项目维护者,
I want 完整追溯产品需求、平台联调和运行恢复证据,
So that 发布时可据实判断首期验收而不会把文档定稿当上线完成。

**Status:** planned-gated — 仅规划，尚未执行；前置条件/证据齐备并据契约复核后才能定稿派发。

**Traceability:** FR-1 至 FR-12；NFR-1 至 NFR-6；SM-1 至 SM-8、SM-C1、SM-C2；AD-1 至 AD-14。

**Dependencies:** 1.18、2.8、3.2、3.3、3.5、3.6、3.7；前序完成表示实际产物与验证通过，不是仅存在规划条目。

**Gates / External Inputs:** 所有适用SQ-1至SQ-5证据和两个目标架构运行验证；外部发布操作另需授权。

**Scope / Deliverables:** 首期验收矩阵/证据索引/未满足项/无秘密交付runbook；不自动推送、部署或发布。

**Acceptance Criteria:**

**Given** 前序功能与平台/运行证据均可读取,
**When** 逐项核对FR/NFR/SM/AD及适用SQ,
**Then** 每项有实际实现、验收场景和证据链接；SM-3真实Seller、SM-7双架构Linux、SM-8实际KMS均有独立证据,
**And** 没有仅靠编号引用、文档存在或mock就算通过，未满足项明确保留。

**Given** 两App/两店铺及冲突、缺链、越权、停用、过期、部分失败矩阵,
**When** 完成跨Epic验收和无秘密检查,
**Then** 成功归属和值完整且失败无任何部分明文，源码/Git/日志/测试/审计/告警/错误无真实秘密,
**And** 正常授权取密输出与错误/日志区分；Vaultwarden/钉钉/远程取密不加入首期验收。

**Given** 部署、审计、备份恢复和指标全部通过或有阻塞,
**When** 整理最终交付判定,
**Then** 只对有实证项目标通过，任一核心安全边界/运行目标未满足不宣称可发布,
**And** 依赖安全核验、版本/架构变更影响及手册复核可追溯，回归覆盖实际改动。

**Given** 证据包完成而尚未获外部发布指令,
**When** 交付本地可审阅结果,
**Then** 提供明确安装/回滚或安全恢复指引、已验证范围和剩余用户/外部动作,
**And** 此Story不自动执行远程部署、推送、合并或第三方资源变更。

**Epic 3 拆分结果：** 共8条；运行契约先于打包/保留/备份，实际备份先于恢复，安全恢复先于RPO/RTO演练；性能测量和恢复演练各自取得证据，最后汇总首期验收。

## Story Coverage and Readiness

下表将需求追溯到具体规划条目；覆盖表示已安排验收场景，不表示实现或验收通过。设计任务先形成契约，`planned-gated`候选必须在适用门槛满足后据实际产物定稿；每个实际发布的操作从首个交付任务就受权限、密文和无秘密审计规则约束。

### FR 到 Story 的追溯

| FR | Story |
| --- | --- |
| FR-1 | 1.5、1.10 |
| FR-2 | 1.14、1.15、2.4、2.8 |
| FR-3 | 1.11、1.16 |
| FR-4 | 1.12、1.16 |
| FR-5 | 2.1、2.2、2.3、2.4、2.5、2.8 |
| FR-6 | 1.1、1.13、1.14、1.17 |
| FR-7 | 1.13、1.14、1.15、1.18、2.6 |
| FR-8 | 1.18、2.5、2.7 |
| FR-9 | 1.16、2.4、2.7、3.5、3.6 |
| FR-10 | 1.1、1.8、1.9、1.16、1.17、2.3 |
| FR-11 | 1.4、1.7、1.8、1.16、2.2、3.3、3.5 |
| FR-12 | 1.4、1.7、1.8、1.9、2.4、2.5、2.6、2.7、3.3、3.4 |

### NFR 到 Story 的追溯

| NFR | Story |
| --- | --- |
| NFR-1 | 1.1、1.2、1.3、1.6、1.13、1.16、1.17、2.4、3.8 |
| NFR-2 | 1.10、1.11、1.12、1.14、1.16、1.18、2.4、2.7 |
| NFR-3 | 2.1、2.2、2.3、2.4、2.5、2.8 |
| NFR-4 | 1.5、1.10、1.11、1.18、2.1 |
| NFR-5 | 1.1、1.8、1.9、1.16、1.17 |
| NFR-6 | 1.3、1.4、1.6、1.7、1.8、3.1、3.2、3.4、3.5、3.7 |

### AD 到 Story 的追溯

| AD | Story |
| --- | --- |
| AD-1 | 1.8、3.2 |
| AD-2 | 1.10、1.13、1.14、1.15、1.18、2.3、2.4、2.7 |
| AD-3 | 1.4、1.7、1.8、1.16、3.3、3.5 |
| AD-4 | 1.5、1.10、1.11、1.12、1.14、1.16、2.4 |
| AD-5 | 1.2、1.11、1.12、1.13、1.16、1.18、2.4、2.6、2.7 |
| AD-6 | 1.2、1.3、1.6、1.13、1.16、2.4、3.4、3.5 |
| AD-7 | 1.13、1.14、1.15、1.18、2.4、2.6、2.7 |
| AD-8 | 1.1、1.5、1.16、3.6 |
| AD-9 | 2.1、2.3、2.4、2.5、2.7、3.5 |
| AD-10 | 2.1、2.2、2.3、2.4、2.5 |
| AD-11 | 1.5、1.10、2.1、2.4、2.6、2.7 |
| AD-12 | 1.4、1.7、1.8、1.9、2.4、3.3、3.4 |
| AD-13 | 1.1、1.8、1.16、1.17、2.3 |
| AD-14 | 1.3、1.4、1.6、1.7、1.8、3.1、3.2、3.4、3.5、3.7 |

### SM 到 Story 的追溯

| SM | Story |
| --- | --- |
| SM-1 | 1.10、1.14、1.15、2.4、2.8 |
| SM-2 | 1.11、1.12、1.16、1.18 |
| SM-3 | 2.1、2.2、2.3、2.4、2.5、2.8 |
| SM-4 | 1.13、1.14、1.15、1.17、1.18、2.6、2.7、2.8 |
| SM-5 | 1.16、1.18、2.4、2.7、2.8、3.6 |
| SM-6 | 1.4、1.7、1.8、1.9、1.16、2.2、2.8、3.3、3.5 |
| SM-7 | 1.7、1.8、3.2、3.4、3.5、3.7 |
| SM-8 | 1.5、1.6、1.18、3.2、3.4、3.5、3.7 |
| SM-C1 | 1.6、1.17、1.18、2.8、3.8 |
| SM-C2 | 1.7、1.16、1.18、2.8、3.5、3.8 |

### SQ 到 Story 的追溯

| SQ | Story |
| --- | --- |
| SQ-1 | 1.5、2.1、2.8 |
| SQ-2 | 1.1、1.8、1.16、1.17、2.3、2.4 |
| SQ-3 | 1.2（当前值/结果包设计）、1.3（身份）、1.6（当前值/结果包隔离实证）；2.4/2.5/2.7消费结果保护子门槛；3.1、3.4、3.5（恢复） |
| SQ-4 | 1.4、1.7、2.2、3.1、3.3、3.4、3.5 |
| SQ-5 | 3.1、3.6、3.7 |

### 当前下一步

先执行可以独立开展的 Story 1.1（CLI 契约）、1.2（密文保护契约）、1.3（身份生命周期契约），随后按依赖完成 Linux 运行设计和隔离证据。需要确认目标 TikTok 市场/App 类型以及准备隔离 Linux/OpenBao 与受控平台环境时，按对应 Gates 取得真实输入；本轮不臆造配置或代执行外部变更。

完整验证结论见 [CE-VALIDATION.md](CE-VALIDATION.md)。34 条规划均未执行；当前不具备把整套计划派发开发或标记为 ready-for-dev 的证据。




