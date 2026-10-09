---
title: Epic 1 本地核心交付与延期边界
status: local-scope-complete
created: 2026-10-09
updated: 2026-10-09
---

# 本轮范围

2026-10-09 用户明确选择：先实现可本地验证部分，Linux/OpenBao/TikTok 验收保持阻塞；Epic 1 本轮只做平台无关手工核心，TikTok profile 延后到 Epic 2；构建与测试用 Docker。此前 CE 的生产门槛未被伪造通过。本文件是本轮范围与证据入口；原 epics.md 中 18 条完整任务的开发验收不能全部标记 done。

本轮包含：严格 stdin 和隐藏 TTY CLI、Unix peer UID 身份、唯一登记与映射变更/解除、安装关联、App/shop 导入、确认、三种归属取密、失效/停用/显式恢复、状态与审计检索、SQLite 原子存储和重启、并发与失败保护、fixture 模式 Docker 镜像与启动配置。

fixture profile 固定虚构业务组合，管理 Token 不输出。业务 DB 存密文与状态；开发 key 独立生成保存在隔离卷中。生产启动缺少 OpenBao 配置时拒绝；没有把本地 AES fixture 替换为生产 KMS。SQLite fixture 以每实体一张 record 表和全状态事务保存，应用负责关系唯一性，仓储 revision CAS 拒绝第二写者覆盖。这是本地验证实现；生产结构性迁移/索引与容量优化仍需在生产门槛下评估。

| 原任务 | 本轮实现/证据 | 未完成边界 |
| --- | --- | --- |
| 1.1 | 23 个本地操作、请求 schema、CLI 校验、虚构样例 | 授权/刷新/平台验证命令的完整生产细化 |
| 1.2 | 当前值保护端口、fixture AAD/版本/用途测试 | OpenBao Transit、待处置平台结果包完整契约 |
| 1.3 | 可替换保护端口，非生产启动拒绝 | AppRole 身份发放/续期/回收 |
| 1.4/1.7 | Docker 非 root、无网络、Unix UID 和进程测试 | Ubuntu 24.04/systemd/账户策略及主机拒绝采集实证 |
| 1.5 | 核心可信 ProviderRegistry；第二虚构 profile 复用测试；本地公开 fixture | TikTok 市场/App/profile 交给 Epic 2 |
| 1.6 | 本地绑定交换拒绝与重启证据 | 隔离 OpenBao 两用途交换/密钥轮换实证 |
| 1.8–1.16/1.18 | 平台无关手工用例、完整取密、版本状态、事务、审计 | 在真实 Linux/OpenBao 基线下再次验收 |
| 1.17 | Linux 伪终端隐藏输入与取消恢复测试 | 目标主机人工验收 |

完整 Epic 1/生产验收保持待完成；本轮本地核心完成与原规划的全量 Epic done 是两种不同范围。真正环境就绪后须补齐延期证据再更新原 CE/sprint 门槛，不用 fixture 测试替代。
