---
title: '独立 TikTok Shop HTTPS Callback 服务'
type: 'feature'
created: '2026-10-13'
status: 'in-progress'
baseline_commit: '278b849c1528324cb8c2a53e8c4585740d23be45'
route: 'dispatch'
review_loop_iteration: 0
context:
  - 'D:/code/github/third-party-api-key-hub/_bmad-output/implementation-artifacts/spec-epic-2-development.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** 当前服务只有本地 Unix Socket 管理入口，没有独立 HTTPS 入口承接 TikTok Shop 授权回调，也没有按多个 App 配置 `app_key/app_secret` 的运行时模型。

**Approach:** 增加独立 callback adapter 和 `ttcallback` 进程。每个 App 以唯一 `app_id` 配置 `app_key`，`app_secret` 只从环境变量或受限 secret 文件加载；授权入口生成一次性 state，回调根据 state 找回 App 并由服务端兑换 Token。Token 保存通过接口交给现有受保护存储，callback 层不把秘密写日志、响应或明文文件。

## Boundaries & Constraints

**Always:** callback listener 与 Unix 管理 listener 使用独立 mux；state 至少 256 bit、短期有效且只能消费一次；客户端不能提交或覆盖 app_secret；多 App 通过 state 绑定，不能由回调参数任意选择密钥；Token 兑换在服务端完成；HTTPS 由证书/私钥启动参数提供；错误响应不回显 code、state、Token 或上游原文。

**Never:** 不把 secret 放入 URL、state、日志、命令行参数或响应；不开放管理/取密路由；不把多个 App 合并成一组全局密钥；不把内存 token 存储宣称为生产持久化；不在本任务内修改 TikTok 控制台或真实第三方资源。

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|----------------------------|----------------|
| 生成授权链接 | 已配置 app_id | 返回含 app_key、redirect_uri、一次性 state 的授权 URL | 未知 app 或配置无效返回 404/配置错误 |
| 成功回调 | 有效 state、code | 服务端用对应 App secret 兑换并交给 TokenSink，响应通用成功页 | 不返回 token/code |
| 重放回调 | 已消费 state | 不再调用上游，返回通用失败页 | 记录非敏感拒绝事件 |
| 多 App 回调 | state 分别属于 App A/B | 各自使用对应 secret | 不允许跨 App 兑换 |
| 上游失败 | code 缺失、拒绝或 token 接口失败 | 不保存结果，返回通用失败页 | 不回显上游响应 |

</frozen-after-approval>

## Code Map

- `internal/adapters/local/server.go` -- 现有 Unix Socket 管理入口；callback 不复用其 mux。
- `internal/application/provider.go` -- 当前只有 fixture 静态 profile；本任务不把 callback 密钥放进请求数据。
- `cmd/hubd/daemon.go` -- 当前仅 fixture 启动；新增独立 `cmd/ttcallback`，避免改变 fixture 安全边界。
- `internal/adapters/callback/` -- 新增多 App 配置、state、Token 兑换和 HTTP handler。
- `deploy/tiktok-callback/` -- 新增配置示例与 systemd/HTTPS 运行说明。

## Tasks & Acceptance

**Execution:**
- [ ] `internal/adapters/callback/config.go` -- 解析并校验多 App 配置，解析 env/file secret -- 防止全局密钥和明文配置。
- [ ] `internal/adapters/callback/state.go` -- 实现一次性、过期 state -- 防回调重放和跨 App 使用。
- [ ] `internal/adapters/callback/server.go` -- 实现 authorize/callback/health 路由和服务端 Token 兑换 -- 隔离公开入口。
- [ ] `internal/adapters/callback/*_test.go` -- 覆盖多 App、重放、错误和 secret 不泄漏 -- 固化边界。
- [ ] `cmd/ttcallback/main.go` -- 提供 HTTPS ServeTLS 入口 -- 独立进程部署。
- [ ] `deploy/tiktok-callback/config.example.json` -- 提供多 App 配置样例 -- 明确 secret 只引用 env/file。

**Acceptance Criteria:**
- Given 两个已配置 App，when 分别访问授权入口，then 得到不同 state 且回调只使用各自的 app_secret。
- Given callback 收到有效 code，when 上游兑换成功，then TokenSink 收到结果且 HTTP 响应不含 code、state、Token 或 secret。
- Given state 已消费、过期或未知，when 再次回调，then 上游兑换调用次数为零。
- Given 配置包含 inline secret、重复 app_id、缺失 key 或不完整 secret 引用，when 启动，then 服务拒绝启动。
- Given HTTPS 证书和私钥，when 启动 `ttcallback`，then 只监听 callback 服务地址，不暴露 Unix 管理路由。

## Implementation Notes

- 已实现 `internal/adapters/callback`：多 App 配置、环境变量/0600 文件 secret、持久化一次性 state、授权 URL、callback、服务端 Token 兑换、重定向阻断和 AES-GCM 文件 sink。
- 已实现 `cmd/ttcallback`：独立 HTTPS `ServeTLS` 进程；不复用 `hubd` 的 Unix Socket 管理 mux。
- Seller 授权 URL 的 provider-specific 参数通过 `authorize_params` 配置；默认仅在没有 `service_id` 时加入 `app_key`，避免把 Seller URL 与其他 TT 授权类型混用。
- 现有 hub 的生产 OpenBao/凭据域尚未提供 `TokenSink` 实现，因此命令默认使用独立加密文件 sink；真实生产接线需替换该接口并保留同样的秘密不落日志约束。
- 2026-10-13：WSL Go 1.27.2 执行 callback 覆盖率 80.4%，`go test ./...` 与 `go vet ./...` 通过。
- 2026-10-13：补齐 `0.3.0` 双架构发布包和安装流程，修复 TLS 启动失败后不退出的问题；最终包完成哈希、架构及 amd64 非 root HTTPS/双 App 模拟兑换验证。详细证据及未完成的生产接线/真实平台验收见 [发布包验证](https-callback-release-validation.md)。

## Verification

**Commands:**
- `go test ./internal/adapters/callback ./cmd/ttcallback` -- expected: all tests pass.
- `go vet ./internal/adapters/callback ./cmd/ttcallback` -- expected: no diagnostics.
