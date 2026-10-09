# Epic 1 Context: 运维可安全导入凭据，业务脚本可按归属取密

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

在受控 Linux 主机上交付一个端到端的手工凭据管理闭环：运维通过 CLI 登记平台 App、维护店铺归属、导入并维护 App/店铺凭据；业务脚本按品牌、内部店铺或外部店铺取得完整且归属正确的业务凭据 JSON。所有操作从本 Epic 起即受本地主机权限、OpenBao 密文保护、事务一致性、完整结果校验和无秘密审计约束；本 Epic 不依赖真实授权流程或自动刷新。

## Stories

- Story 1.1: 明确运维命令与业务脚本的统一 CLI 契约
- Story 1.2: 明确当前凭据与待处置结果的密文保护契约
- Story 1.3: 明确 OpenBao 服务身份的发放、续期与回收契约
- Story 1.4: 明确受控 Linux 主机的运行、权限和审计采集契约
- Story 1.5: 核实 TikTok 手工导入所需字段用途与业务凭据组合
- Story 1.6: 取得当前值/待处置结果交换拒绝与身份生命周期的隔离证据
- Story 1.7: 取得 Linux 身份、权限和入口拒绝采集的隔离证据
- Story 1.8: 运维可启动受控本地服务并检查安全运行状态
- Story 1.9: 运维可检索非敏感审计并发现审计降级
- Story 1.10: 运维可登记并查询唯一的平台 App
- Story 1.11: 运维可维护外部店铺与品牌归属
- Story 1.12: 运维可维护品牌与内部店铺的一对一映射
- Story 1.13: 运维可从标准输入安全导入 App 级凭据
- Story 1.14: 运维可安全导入店铺凭据并形成独立安装关联
- Story 1.15: 运维可检查状态并确认未知有效期凭据可用
- Story 1.16: 业务脚本可按完整归属取得全部必需业务凭据
- Story 1.17: 运维可通过隐藏 TTY 输入导入凭据
- Story 1.18: 运维可停用失效凭据并在显式替换后恢复取密

## Requirements & Constraints

- 支持平台/App 登记、安装关联、外部店铺→品牌映射及品牌↔内部店铺双向唯一映射；所有唯一性冲突整笔拒绝，映射缺链或变更后立即禁止旧归属取密。
- 手工导入必须明确 App 级或安装级归属、凭据 type/value；同目标同类型覆盖需显式替换。未知有效期以 `expiry_known=false`、`expires_at=null` 保存，只有当前版本运维确认才可取密；明确失效执行停用。
- 按品牌、内部店铺或外部店铺查询时，唯一 App/店铺可自动选择，多项必须显式选择；完整归属、权限和所有业务必需凭据均满足后才一次性返回，任一必需项失败则整体无明文。
- stdout 始终是稳定 `contract/v1` JSON，成功退出码 0、失败退出码 1；失败仅含稳定 `error` 与安全 `message`，诊断写 stderr。禁止秘密进入参数、日志、审计、告警、错误、源码或测试数据。
- 仅服务所在 Linux 主机的白名单账户可经 Unix Socket 取密（read）或变更（manage）；每请求及输出明文前重新核对内核 UID、账户白名单和策略。无远程取密、管理 TCP 或通过公开回调扩大权限。
- 成功和失败的登记、映射、导入、查询、确认、失效、停用/恢复等操作均写非敏感审计；至少保留一年。审计写入失败不回滚业务，但须告警并使健康状态降级。
- Epic 交付须在目标 Linux/systemd 和隔离 OpenBao 上验证；SQ-1（手工导入类型/用途/组合）、SQ-2（CLI schema）、SQ-3（密文与身份保护）、SQ-4（部署/权限/审计采集）证据是实现门槛。

## Technical Decisions

采用 Go 六边形模块化单体：单个 `hubd` 持有领域规则和唯一 application 变更入口，`hub` 仅负责 CLI、契约、安全输出及 Unix Socket 客户端；适配器通过端口接入 SQLite、OpenBao Transit 和平台实现，采用 `cmd/`、`internal/` 布局。

本地协议为路径型 Unix stream socket `/run/api-key-hub/hub.sock`，HTTP/1.1 JSON `/v1/operations`，每连接一个请求，最大 1 MiB；请求拒绝未知/重复字段和超限体。共享 `contract/v1` 定义操作、DTO、错误码和兼容规则。所有领域变更在写门内同一 SQLite 事务提交，查询持读门直至完整结果和权限复核；使用 `row_revision` CAS、凭据 `generation` 和 App `authorization_epoch` 防止迟到或跨对象更新。

SQLite 只存密文及非敏感元数据。当前值使用 `purpose=current-credential` 绑定中心目标、type、generation；未提交的平台结果使用 `purpose=pending-provider-result` 绑定 operation/App/epoch/profile/依赖快照，两种用途必须互换失败。生产 OpenBao Transit 采用 TLS 和最小权限 AppRole，SecretID 通过 systemd credential 文件提供；KMS 或事务失败保留旧记录并不得把依赖故障当作平台失效。

凭据可用性由期限、运维确认、平台验证、停用和版本证据等正交字段计算。替换/重新授权/刷新成功递增 generation 并清理旧证据；停用不会被刷新、验证或映射恢复自动解除，恢复必须有停用后合格替换/重授权的事务证据并重新检查当前有效性、完整映射和 manage 资格。平台网络调用不得持领域写门，外部副作用不确定时进入受保护待处置路径。

## UX & Interaction Patterns

CLI 无终端 UI；交互提示只写 stderr，stdout 保留单个 JSON 结果。导入支持严格单个 JSON stdin（读至 EOF）及显式 interactive TTY 隐藏输入；秘密不得作为命令参数、不得回显，取消后恢复 TTY。业务查询先完整读取、解密、验证和权限复核，再一次性序列化完整结果，不流式输出或返回部分凭据。

## Cross-Story Dependencies

1.1 先固定统一命令/schema/错误和兼容契约；1.2、1.3 分别固定当前值/待处置结果保护与 OpenBao AppRole 身份规则；1.4、1.5、1.6、1.7 形成 Linux、平台字段和隔离证据门槛。1.8、1.9 消费运行与审计契约，1.10–1.18 在这些契约和 SQ-1 至 SQ-4 证据上实现登记、映射、导入、查询及停用恢复。Epic 1 可独立交付手工管理价值；Epic 2 的授权/刷新实现不得被本上下文视为已核实或已实施。
