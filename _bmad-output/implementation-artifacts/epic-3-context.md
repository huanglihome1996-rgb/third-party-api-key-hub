# Epic 3 Context: 运维可验证发布质量、保留审计并从故障中恢复

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

交付双架构可复现 Linux 包、至少一年审计保留与安全采集、每日一致备份和受控恢复流程，以真实运行及恢复证据判断首期交付质量。发布和恢复后持续满足凭据保密、正确归属与本地主机访问边界。

## Stories

- Story 3.1: 明确审计保留、一致备份和运行恢复的验收契约
- Story 3.2: 运维可获取可复现的双架构 Linux 发布包
- Story 3.3: 运维可保留一年审计并检测系统拒绝与告警
- Story 3.4: 运维可生成每日一致且可核验的完整备份
- Story 3.5: 运维可在关闭取密和调度的状态下恢复服务
- Story 3.6: 验证十个本地并发下完整取密延迟
- Story 3.7: 演练每日备份的 RPO 和人工恢复 RTO
- Story 3.8: 形成首期 Linux 交付与安全验收证据包

## Requirements & Constraints

- 本轮用户已明确先完成平台无关代码与 Docker 验证；生产、真实平台及目标双架构运行验收保留未完成，fixture 不替代这些证据。
- Ubuntu Server 24.04 LTS 上交付 linux/amd64、linux/arm64 版本化归档、构建清单、校验和及操作手册；比较重复构建结果，交叉编译不能替代两架构安装/运行验收。
- 审计至少保留一年，明确时间/清理边界并用历史 fixture 验证。区分服务审计、客户端自报与系统拒绝；未到达服务的拒绝须实际采集。审计故障不回滚业务，白名单告警并降级健康状态；不伪造补写或承诺同磁盘故障零丢失。
- 每日一致业务/审计快照关联策略、配置、schema、构建及 OpenBao 密钥版本；冻结/排空或等价边界，禁止复制写入中的 DB。半成品不替换旧可用备份；密钥恢复材料独立保护，manifest 无秘密。
- 真实 Linux、健康隔离 KMS 下，10 本地并发完整 CLI→服务→解密→输出路径 p95 ≤2s。明确样本、百分位方法、健康条件及冷启动/维护/失败样本，不能排除慢请求。RPO ≤24h、人工 RTO ≤4h；记录故障、最新完整可恢复状态、复核后开放时刻，人工复核与必要等待依约计入。
- 文档/mock/构建不能替代真实平台、Linux/systemd、KMS 及恢复实测；核心边界或指标未满足保留未通过。真实秘密不得进入源码、Git、测试、日志、告警、错误或报告；禁止采集 argv/query/body，测试与生产隔离。外部发布及资源变更另需明确授权。

## Technical Decisions

- Go 模块化单体，hubd 唯一变更入口。已批准 Go 1.27.2 替代旧规划 1.27.1；sqlite 1.60.1、libc 1.77.1、x/sys 0.48.0、OpenBao 2.7.1 锁定。systemd 用目标发行版包，核验指令支持；Node 不是运行依赖。
- 非 root hubd，数据目录 0700/文件 0600，CLI 不可读 DB/bootstrap credential。Unix Socket 以内核身份/root 策略复核 read/manage，无管理 TCP，仅公开受控回调。启动检查运行锁、策略、schema、后端身份/密钥，禁用 core dump/公开调试/内容日志。
- SQLite 本地文件、rollback journal、synchronous=FULL、每连接外键；审计库独立于业务事务。OpenBao 校验 TLS 身份/AppRole 最小授权，生产 SecretID 经 systemd credential 文件，禁用 argv/unit 字面量/生产环境变量。
- 恢复默认关闭取密/调度且跨重启保持；复核账户、完整映射、平台当前值及必要验证/重授权后由 manage 显式开放。缺密钥/身份不符/refresh 不确定保持关闭，停用不自动解除，恢复资格绑定当前版本。
- 未结操作继续阻止 App 轮换；暂存包校验用途/operation/App/epoch/profile/依赖绑定，仅重试本地条件提交。禁止盲重放备份任务或假定旧 refresh 可用；失败无部分明文，不以秘密缓存、HA、跳过 KMS 达标。

## UX & Interaction Patterns

本地主机 CLI，stdout 一次完整验证后的 contract/v1 JSON，stderr 诊断，成功 0/失败 1；非取密结果无秘密，审计检索须 manage。恢复手册明确关闭→核对→显式开放与失败停点。

## Cross-Story Dependencies

依赖 Epic 1/2 权限、加密、审计、状态及平台恢复的实际交付/验证，不把规划条目当验收。3.1 契约→发布/采集/备份→安全恢复→RPO/RTO 演练；性能独立取证，3.8 汇总。

相关实现定稿须消费 SQ-3 密钥/AppRole 恢复、SQ-4 精确部署/采集/保留/存储与 Linux 实证、SQ-5 测量/故障演练契约。最终仍需真实 Seller 联调及适用前序门槛；缺证据保持未通过。
