# Third-Party API Key Hub

用于统一对接第三方平台并管理其 API 凭据的项目基础目录。

## 当前状态

- 已初始化 Git 仓库
- 已安装 BMAD Method 6.12.0（BMM 模块）
- 已配置 Codex 技能到 `.agents/skills`
- BMAD 产物默认输出到 `_bmad-output`

## 开始使用

1. 在项目目录打开 Codex 或其他已配置的 AI 工具。
2. 使用 `bmad-help` 技能查看适合当前阶段的 BMAD 工作流。
3. 复制 `.env.example` 为 `.env.local`，填入本地开发所需配置。

## 凭据原则

- 不把真实 API Key、Secret、Token 或连接字符串提交到 Git。
- `.env.local` 仅用于本机开发，并已加入 `.gitignore`。
- 生产环境应接入专用密钥管理服务，并通过运行时注入凭据。

## 目录说明

| 目录 | 用途 |
| --- | --- |
| `_bmad/` | BMAD 核心与 BMM 模块 |
| `_bmad-output/` | BMAD 生成的规划与项目文档 |
| `.agents/skills/` | Codex 使用的 BMAD 技能 |
| `src/` | 后续第三方平台适配器和密钥管理代码 |

