# Contributing to Gpi

欢迎对 Gpi 的贡献！本指南适用于所有希望参与开发的开发者，包括首次贡献者。

> 本指南参考 [AcmeStack Contributor Guide](https://acmestack.com/docs/contributing/guide/) 的结构整理。

## 目录

- [Contributing to gpi](#contributing-to-gpi)
  - [目录](#目录)
  - [开发环境](#开发环境)
  - [代码规范](#代码规范)
  - [提交规范](#提交规范)
  - [分支与工作流](#分支与工作流)
  - [Pull Request 流程](#pull-request-流程)
  - [测试](#测试)

## 开发环境

要求：Go ≥ 1.26（见 `go.mod`）。

```bash
# 构建控制面 gpi 与节点 agent gpilet
make build

# 运行全部测试
make test

# 静态检查
make vet
```

开发时使用 `GPI_HOME` 指向一个临时状态目录，避免污染真实环境：

```bash
export GPI_HOME=/tmp/gpi-dev
```

## 代码规范

- 保持"零不必要依赖"：云侧、SSH、HTTP 尽量用 Go 标准库。
- 所有导出类型/函数必须有文档注释（`go vet` 会检查）。
- 提交前运行 `gofmt -l .` 确保格式统一。
- 遵循现有代码约定（错误处理、命名、包结构），参见 [docs/gpi-architecture.md](gpi-architecture.md)。

## 提交规范

遵循 [Conventional Commits](https://www.conventionalcommits.org/) 规范：

```
<type>(<scope>): <subject>

<body>
```

常用 type：

- `feat:` 新特性
- `fix:` 修复
- `docs:` 文档
- `chore:` 杂项（构建、CI、依赖等）
- `ci:` CI 配置
- `test:` 测试
- `refactor:` 重构

示例：

```
feat(server): add configurable API response key case style
docs: add clickable links for referenced docs in README
```

## 分支与工作流

- `main` 为主分支，所有 PR 合并到 `main`。
- 功能开发：从 `main` 切出分支（`feat/xxx`、`fix/xxx`）。
- 版本发布：打 `v*` tag 触发 [Release workflow](../../.github/workflows/release.yml) 自动构建发布。

## Pull Request 流程

1. Fork 本仓库并创建你的分支。
2. 提交变更（遵循提交规范）。
3. 推送分支后发起 PR 到 `main`。
4. **签署 CLA**：你的第一个 PR 会触发 CLA assistant bot 评论，回复 `I have read the CLA Document and I hereby sign the CLA` 即可签署；回复 `recheck` 可重新检查签署状态。CLA 全文见 [.github/CLA.md](../../.github/CLA.md)，官方说明见 [AcmeStack Contributor License Agreement](https://acmestack.com/docs/contributing/contributor-license-agreement/)。请确保 `git config user.name` 与 GitHub 用户名一致。
5. 确保 CI（build/vet/test + 交叉编译）通过。
6. 维护者 review 并合并。

## 测试

- 新增功能应附带单元测试（测试文件与被测代码同包，如 `internal/server/server_test.go`）。
- 运行全量测试：`make test`。
- 涉及 HTTP API 的改动建议补充 handler 级测试（参考 `internal/server/server_test.go`）。
