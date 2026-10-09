---
title: 已采用架构技术核实
status: researched
created: 2026-10-08
updated: 2026-10-08
---

# 技术依据与限制

核实日期：2026-10-08。来源为官方资料、官方模块代理与项目发布信息；资料核实支持所采用技术存在及基本能力，不证明安装、漏洞扫描、集成或性能验收。用户已接受 R-1 至 R-10，技术选择以架构基线为准。

| 已采用技术／机制 | 核实结果 | 权威依据 |
| --- | --- | --- |
| Go | 下载 JSON 返回稳定版本 go1.27.1，作为构建基线；服务项目可用 cmd/internal 分开入口与私有包。 | [稳定下载元数据](https://go.dev/dl/?mode=json)、[官方模块布局](https://go.dev/doc/modules/layout) |
| SQLite | 支持事务；唯一关系与原子变更可由 SQLite 与用例共同约束。首期单实例、本地文件的方案是本项目选择，不是官方性能承诺。 | [事务性质](https://www.sqlite.org/transactional.html)、[事务语义](https://www.sqlite.org/lang_transaction.html)、[外键/同步设置](https://www.sqlite.org/pragma.html) |
| modernc.org/sqlite | 官方模块代理 latest=v1.60.1，发布时间 2026-09-29；该版本 go.mod 要求 Go 1.26.0、modernc.org/libc v1.77.1、x/sys v0.48.0。纯 Go 驱动用于已接受的 Linux 目标；本项目未运行交叉构建。 | [模块代理元数据](https://proxy.golang.org/modernc.org/sqlite/@latest)、[固定版本 go.mod](https://proxy.golang.org/modernc.org/sqlite/@v/v1.60.1.mod)、[固定版本文档](https://pkg.go.dev/modernc.org/sqlite@v1.60.1) |
| 版本冲突处理 | pkg.go.dev 的 latest 提示与缓存正文存在差异；版本锁定按实时官方模块代理及固定版本 go.mod。不得从文档示例中不同的 libc 版本推导本版本依赖。 | 同上固定版本 go.mod；按 implementation 时 go.sum 和构建核验继续检查。 |
| OpenBao | 官方发布 API 显示 v2.7.1 于 2026-10-01 发布，采用该版本；不是“已经在用户环境运行”。 | [发布记录](https://github.com/openbao/openbao/releases/tag/v2.7.1)、[固定发布元数据](https://api.github.com/repos/openbao/openbao/releases/tags/v2.7.1) |
| Transit | 提供加解密及密钥管理能力，应用保存密文；官方 API 提供 context/associated_data 机制，具体绑定编码在安全存储 Story 前固定并验证。选择 Transit 而非 KV 是本项目已接受的事务边界，密钥/版本恢复另行验证，不使用导出明文密钥的路径。 | [官方 Transit 文档](https://openbao.org/docs/secrets/transit/)、[Transit API](https://openbao.org/docs/api/secret/transit/) |
| OpenBao 身份和 TLS | AppRole 是已采用的机器身份方案，listener 支持 TLS 配置；SecretID 发放/轮换/回收、登录 Token 续期/重新认证和最小策略尚未实施。 | [AppRole](https://openbao.org/docs/auth/approle/)、[listener 配置](https://openbao.org/docs/configuration/listener/tcp/) |
| Linux 本地身份 | 路径 Unix stream socket 可用 SO_PEERCRED 获取连接时的内核身份；请求体用户名不能替代它。每请求新连接、UID/账户匹配和策略复核是本中心规则。 | [Linux unix(7)](https://man7.org/linux/man-pages/man7/unix.7.html) |
| systemd credential | LoadCredential 可向服务提供受保护只读 credential 文件，避免生产秘密注入普通环境变量；需核验 Ubuntu 目标包支持所用指令。 | [systemd.exec(5)](https://man7.org/linux/man-pages/man5/systemd.exec.5.html) |
| Ubuntu | 24.04 LTS 仍有标准安全维护，官方发布说明到 2029-05-31；已选为首期验收基线，未表示它是最新 LTS 或用户现有发行版。 | [官方发布说明](https://documentation.ubuntu.com/release-notes/24.04/)、[发行周期](https://ubuntu.com/about/release-cycle) |

## 平台依据

TikTok Shop 的既有官方线索及适用性限制保留在 [PRD addendum](../../prds/prd-third-party-api-key-hub-2026-10-08/addendum.md#tiktok-shop-官方资料核对2026-10-08)。本轮不把它们升级为目标市场/App 的已核实契约，也未联调真实 App。

Q-7 在平台接入 Story 前须核实：目标市场和 App 类型、Seller 授权/回调校验、授权店铺与 shop_cipher、Token 有效期与轮换、验证/失效分类，以及远端不确定结果如何安全处理。平台端撤销不是本中心首期本地停用的实施前提。

## 验证计划

- 架构阶段：机械 lint、PRD/技术约束对齐、独立一致性/技术依据审查；不将文件存在当成设计被用户接受。
- 存储/生命周期：用虚构秘密验证唯一性、事务回滚、映射变更与取密竞争、row_revision 条件提交、generation 密文绑定、停用/恢复和未知有效期。验证密文交换、KMS 故障及失配时无部分秘密输出。
- Linux 边界：核验实际 UID、未授权账户、Socket 权限、策略撤销及既存连接；检验 CLI 缺项/TTY 中断和 stdout/stderr。
- 回调/平台：重复、迟到及不匹配会话，兑换/轮换超时与崩溃恢复，平台响应秘密不会进入错误或日志；受控真实联调由独立接入计划批准执行。
- 部署/恢复：目标架构构建、systemd、最小权限、备份恢复和密钥版本恢复、一年保留及审计故障告警、容量/SLO 演练；只按实际输出认定通过。
