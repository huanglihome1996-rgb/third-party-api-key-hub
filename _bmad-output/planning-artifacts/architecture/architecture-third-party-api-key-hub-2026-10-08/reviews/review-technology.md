---
title: Technology validity review
reviewed: 2026-10-08
updated: 2026-10-08
verdict: pass
scope: named versions, mechanisms, platform fit, evidence boundaries
---

# 技术有效性审查

当前结论：通过。技术候选与资料中的版本可核实，整体适合首期单节点 Linux 服务；原 Socket 权限及身份续期措辞问题已修复，可交付为待用户批量接受的架构草案。没有把研究结论误写成生产集成或性能已通过。下列初审发现保留为历史，最终状态见复查记录。

## Finding T-1 — 高：非 root 服务无法在原指定目录创建 Socket

- **位置**：原 AD-3 与 AD-14。
- **证据**：AD-3 原规定目录 `root:hub-clients 0750`，AD-14 规定固定非 root `hubd`。Unix pathname Socket 的创建要求父目录有 write 和 search 权限；原目录既不给 hubd 所有者写权限，也不给组写权限。即使 Socket 最终要求 `hubd:hub-clients 0660`，该 Socket 仍无法首先由服务创建。
- **影响**：按契约直接实施的服务在 bind 时失败。不能留给两个部署/服务实现者各自解释。
- **处理**：明确由 systemd 管理 RuntimeDirectory，实际目录 `hubd:hub-clients 0750`，服务负责创建 `0660` Socket；客户端组保持无目录写权限。另一可行路径是 root 的 systemd socket activation 创建并传递监听 fd，但不能仅指定 root 目录而假定服务可 bind。root 所有的授权策略与运行目录分开。
- **状态**：已通知作者，作者已选择前一种修复；最终复查应确认实际规则文本落地。
- **权威依据**：[Linux unix(7)](https://man7.org/linux/man-pages/man7/unix.7.html)，Pathname socket ownership and permissions。

## Finding T-2 — 低：区分 AppRole SecretID 轮换与登录 Token 续期

- **位置**：technology-evidence.md 的“OpenBao 身份和 TLS”行，及 Deferred 的 AppRole 生命周期。
- **证据**：资料写“SecretID 发放/续期/回收”。官方 AppRole API 为 SecretID 提供生成、查阅、销毁等操作，SecretID 有 TTL/使用次数；登录后取得的 Token 才有 renewable 及 renewal 生命周期。不能把 Token 续期当作 SecretID 续期。
- **处理**：措辞改为“SecretID 发放/轮换/回收及登录 Token 续期/重新认证”；在部署 Story 中验证 SecretID 到期、使用耗尽、daemon 重启和 Token 最大 TTL 后的重新登录。无需在本轮引入具体 Agent 或 SDK。
- **状态**：小幅文档修正，不影响 OpenBao/AppRole 的技术适用性。
- **权威依据**：[AppRole 概览](https://openbao.org/docs/auth/approle/)、[AppRole API](https://openbao.org/docs/api/auth/approle/)。

## 已核实与保留的实施门槛

1. 本审查独立读取 Go 官方下载 JSON，稳定候选为 `go1.27.1`；官方模块代理确认 `modernc.org/sqlite v1.60.1` 的 go.mod 要求 Go `1.26.0`、`modernc.org/libc v1.77.1`、`golang.org/x/sys v0.48.0`。Go 1.27.1 满足该最低要求。未运行构建、漏洞扫描或目标架构测试。
2. OpenBao 官方 GitHub 固定发布 API 返回 `v2.7.1`、发布日 2026-10-01，非 draft、非 prerelease。版本选择不是安装或配置证明。
3. Transit 能满足 Seal/Open 与目标绑定：官方 2.7.x API 的 AEAD 加解密支持 `associated_data`，derived key 支持 `context`。将目标、类型、generation 规范编码为 AAD 或加密包并复核具有技术可行性；目前没有决定具体编码，也没有实现或交换密文测试。原 Deferred 已把该契约集中到安全存储 Story，不能提前声称绑定经过验证。建议技术证据补充 [Transit API](https://openbao.org/docs/api/secret/transit/)。
4. “生产密钥不导出”可通过 Transit 非 exportable key 与权限落实。不要误用 `/transit/backup/:name` 作为默认恢复步骤：官方接口返回明文 key backup，需要另行允许 plaintext backup，与当前不导出规则不一致。原架构并未要求此接口，因此本项不是现有阻塞；部署 Story 应采用符合规则的 OpenBao 存储快照及独立恢复/unseal 材料，并验证旧密文恢复。
5. SO_PEERCRED 确实提供连接时的 Linux 内核身份，不能等同于请求体用户名或持续读取进程实时身份。每请求新连接与政策重新读取适合已声明的主机受控账户/root 信任边界。
6. systemd `LoadCredential` 支持向目标服务用户提供只读 credential，相关功能始于 systemd 247；部署仍应核验 Ubuntu 24.04 维护包及具体 unit 指令。credential 交付不是 SecretID 自动轮换机制。root 管理源文件及必要时加密源文件属于部署契约。
7. SQLite 本地单实例、rollback journal、FULL 同步、每连接外键及一致备份没有发现机制冲突。10 并发、p95/RPO/RTO 明确标为待接受目标，而非官方或已测保证。Linux 权限/systemd 验收必须在 Linux；Windows 纯 Go 领域检查不能替代该验收。
8. TikTok Seller/App/市场、state 和轮换契约明确保持未核实，不将其他授权资料或通用 OAuth 模板作为该适配器已确定行为，资料边界正确。

## 来源与方法

阅读 ARCHITECTURE-SPINE.md、technology-evidence.md、recommended-decisions.md；对版本元数据与 Transit/AppRole/systemd/Linux 能力进行官方来源只读核实。未创建/修改任何外部资源，未修改被审查文档。SQLite/Ubuntu 来源由作者已核实的官方证据支持；未将作者报告的资料读取升级为本审查的构建/运行证据。

官方版本：[Go 下载元数据](https://go.dev/dl/?mode=json)、[sqlite 固定 go.mod](https://proxy.golang.org/modernc.org/sqlite/@v/v1.60.1.mod)、[sqlite latest](https://proxy.golang.org/modernc.org/sqlite/@latest)、[OpenBao 固定发布 API](https://api.github.com/repos/openbao/openbao/releases/tags/v2.7.1)。

机制：[Transit API](https://openbao.org/docs/api/secret/transit/)、[AppRole](https://openbao.org/docs/auth/approle/)、[AppRole API](https://openbao.org/docs/api/auth/approle/)、[systemd.exec(5)](https://man7.org/linux/man-pages/man5/systemd.exec.5.html)、[unix(7)](https://man7.org/linux/man-pages/man7/unix.7.html)。

## 2026-10-08 修复后复查

- **T-1 已解决**：实际读取新版 AD-3，RuntimeDirectory 明确由 systemd 创建为 `hubd:hub-clients 0750`，服务创建 `hubd:hub-clients 0660` Socket；hubd 有目录创建权限而客户端无写权限，root 授权配置另存。
- **T-2 已解决**：technology-evidence.md 明确区分 SecretID 发放/轮换/回收与登录 Token 续期/重新认证。Deferred 的“AppRole 发放/续期”是上位概括，建议后续细化时沿用上述精确区分，不构成当前阻塞。
- **Transit 证据补充已落实**：官方 API 链接、context/associated_data 机制和不得通过明文 key export 恢复的边界已进入技术依据，具体绑定编码、系统交付和恢复测试保持 Story 前门槛。
- **新增版本与外部副作用规则可实施**：密文只绑定秘密值 generation，状态 CAS 使用 row_revision、授权意图使用 authorization_epoch；元数据变更不会凭空令原密文无法解密。SQLite 条件事务可承载这些版本规则，App 聚合修订号可用于未知店铺兑换的冲突检测。App 级协调及共享 refresh_resource_id 属于应用规则，不要求 SQLite 或 OpenBao 提供远端 exactly-once；远端成功但 KMS/DB/权限/CAS 失败明确进入待处置，避免虚构可重放能力。
- **限制保留**：通过的是草案技术有效性；尚未执行 Linux 权限验收、密文互换测试、平台轮换联调、性能或恢复演练，也没有证明新版本已无漏洞。相关数值与新技术方向仍待用户接受。

本轮仅更新本审查报告，未修改架构或其他审查者的文件。当前技术 lens 无未解决高/中级阻塞。
