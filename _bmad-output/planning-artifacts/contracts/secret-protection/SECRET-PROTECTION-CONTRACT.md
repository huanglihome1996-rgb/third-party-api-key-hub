---
title: 本地手工核心密文保护交接
status: local-scope-complete
created: 2026-10-09
updated: 2026-10-09
---

# 当前凭据保护

application 拥有 `Protector.Seal/Open(purpose,target,type,valueOrCipher,generation)` 端口，不依赖具体适配器。当前用途仅为 current-credential；target 使用已校验的 scope|App|shop 编码，ID 禁止竖线，app scope 的 shop 为空。generation 为正整数的秘密值版本，与 row_revision/authorization_epoch 分开。获取明文前必须复核完整绑定。

fixture 后端使用随机生成的 32-byte 本地开发 key 和 AES-256-GCM，每次独立随机 nonce；密文包是无 padding 的标准 Base64，内容为 nonce+ciphertext+认证标签。AAD 是 Go JSON 编码的数组 `[1,purpose,target,type,generation]`。这不是 OpenBao 密文格式。开发 key 仅在 fixture 状态卷中 0600 保存，业务 DB 仅保存密文和元数据。该模式显式 `--fixture`，仅用于虚构 fixture-/fake- 值；前缀不是凭据真实性证明，禁止投放真实值。

加密成功后在一致性门内，将密文、generation、状态、恢复资格和对象修订同一 SQLite 事务提交；持久化失败丢弃 candidate，旧记录保留。不缓存查询明文，完整业务组合全部解密与权限复核后才序列化。重新加密不得自行改变业务 generation。

测试覆盖用途、目标、类型、generation 交换拒绝，开发 key 重启读取，以及 DB 事务失败、过时 CAS、无部分秘密结果。OpenBao 的 Transit 编码、后端版本/轮换、TLS、AppRole、pending-provider-result、密钥独立备份恢复未取得实证；原 SQ-3 门槛继续阻塞。这份本地交接不替代 Story 1.2 的完整生产契约，也不证明平台结果包已经实现。
