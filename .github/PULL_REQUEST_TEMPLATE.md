<!-- Use a short, clear title following Conventional Commits, e.g.:
     标题请用简短清晰的描述，遵循 Conventional Commits，例如：
     feat(optimizer): add latency metric
     fix(server): resolve swagger requestBody under snake key style
     docs: add pr template
-->

## What / 变更内容

<!-- Describe what this PR does. / 请描述本次 PR 做了什么： -->
- 

## Why / 为什么

<!-- Explain why this change is needed. / 请说明为什么需要这个变更（解决问题/动机）： -->
- 

## How verified / 测试

<!-- Confirm the following before merging (tick as applicable).
     请在合并前确认以下项（按需勾选）： -->
- [ ] `make build` passes (incl. `go generate`) / 通过（含 `go generate`）
- [ ] `go vet ./...` passes / 通过
- [ ] `go test ./...` passes / 通过
- [ ] Cross-compile passes / 交叉编译通过（`GOOS=linux/darwin GOARCH=amd64/arm64`）
- [ ] Handler-level tests added/updated for API changes / 涉及 HTTP API 时已补充/更新测试
- [ ] Docs updated with version + changelog / 涉及文档时已更新（含版本号与变更记录）

## Related / 关联

- Related issue / PR / 关联 issue / PR：#

## Notes / 其他

<!-- Anything else reviewers should know (performance, breaking changes, migration, etc.).
     其他需要 reviewer 关注的点，如性能影响、破坏性变更、迁移说明等。 -->
- 
