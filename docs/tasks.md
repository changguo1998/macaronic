# Macaronic 里程碑任务清单（阶段 4）

> 本文件对应 [`development-plan.md`](development-plan.md)，拆解 M17–M19
> 为执行级任务。阶段 3（M14–M16）已归档于
> `archive/tasks-phase3.md`。每个 T-ID 完成后将 `[ ]` 改为 `[x]`。
>
> 方言能力以实测探针为准；凡「已实测」标注的判断，均不得凭语法知识推翻。

## 进度总览

| 里程碑 | 预估工作量 | 已打勾 / 总数 |
| --- | --- | --- |
| M17 | 4–6 工时 | 6 / 6 |
| M18 | 5–8 工时 | 6 / 6 |
| M19 | 5–8 工时 | 0 / 5 |
| **M17–M19** | **约 14–22 工时** | **12 / 17** |

## M17 — 运行时预检与 sh 引擎

- [x] T17.1 `engine` 增加可选接口
  `RuntimeChecker { RequiredCommands() []string }`，并在 `analyze.Analyzer.Run`
  中每 stage 探测；缺失即产出 `SevError` 级 Issue（含 stage 与起始行）并跳过
  该 stage 后续分析。验收：缺失与存在两条路径的 table-driven 测试。
- [x] T17.2 仅 shell 家族实现 `RuntimeChecker`，命令与各自 `RunCommand` 的
  `argv[0]` 一致（`bash` / `sh` / `zsh` / `tcsh`）；python / go 不实现。
  验收：断言两者一致性的测试，防止后续漂移。
- [x] T17.3 为 cli 层注册 shell 引擎的测试补运行时守卫，使无该方言的环境
  以 skip 而非硬失败结束。验收：全量测试通过且守卫覆盖所有相关用例。
- [x] T17.4 新增 `#!sh` 引擎：`#!/bin/sh`、`set -eu`、标量 prologue/epilogue
  走 `macaronic codec`；`RunCommand` 为 `["sh", "run.sh"]`。
  验收：标量四类型跨块 e2e 通过。
- [x] T17.5 sh 的 list 契约类型在 `AnalyzeDetailed` 中产出拒绝诊断（
  `Var` + `Span` + 消息），因此自动获得原始 `.mac` 行号并抑制该 stage 的 M12
  警告。验收：使用 list 时 check 非零退出、行号精确、消息含类型名。
- [x] T17.6 运行 M17 固定质量闸门并创建独立提交。
  验收：gofmt、vet、test、race、diff-check、markdownlint 全部通过。

## M18 — zsh 引擎

- [x] T18.1 新增 `#!zsh` 引擎骨架：`#!/usr/bin/env zsh`、`set -eu`、
  `RunCommand` 为 `["zsh", "run.sh"]`、诊断正则 `^(?:\./)?([^:]+):(\d+): (.*)$`。
  验收：非首行运行时错误回映到原始 `.mac` 行号。
- [x] T18.2 数组 prologue 实现：`name=()` +
  以 `while IFS= read -r -d ''` 逐元素读入并把元素追加到同名数组，配
  `< <(macaronic codec read-list "f" "t")`（zsh 无 `mapfile`，已实测）。
  验收：四种数组 prologue 产物断言 + e2e。
- [x] T18.3 路径与类型参数一律双引号。验收：回归测试覆盖含 `[]` 的状态文件名，
  证明不会因 `nomatch` 静默产生空数组（已实测：未引用时 `<(...)` 内失败不影响
  外层退出码）。
- [x] T18.4 数组 epilogue 前插入 `(( ${+name} )) || name=()`。验收：只写不读的
  list 在 `set -u` 下不失败，且与 bash 的 0 参数行为一致。
- [x] T18.5 `bash → zsh` 跨方言集成：含带空格 `str[]` 的写入、读取修改、再写入，
  最终 codec 值正确。验收：跨引擎 E2E 通过，标量无回归。
- [x] T18.6 运行 M18 固定质量闸门并创建独立提交。
  验收：gofmt、vet、test、race、diff-check、markdownlint 全部通过。

## M19 — csh 引擎

- [ ] T19.1 新增 `#!csh` 引擎，`RunCommand` 为 `["tcsh", "-e", "run.csh"]`。
  验收：命令失败使 stage 非零退出（对照：不加 `-e` 会继续执行并以 0 退出，已
  实测）。
- [ ] T19.2 标量注入：`set name = "`macaronic codec read 'f' 't'`"` 与
  `macaronic codec write 'f' 't' "$name"`，路径与类型单引号引用以规避 `[]`
  glob。验收：含空格 `str` 的四类型跨块 e2e 通过。
- [ ] T19.3 csh 的 list 契约类型报 error 拒绝（同 sh）。验收：check 非零退出、
  行号精确；理由（NUL 流按空白切分导致静默错位）写入消息或文档。
- [ ] T19.4 `ParseDiagnostics` 返回空并注释说明；运行时失败降级为 `stage N` 加
  原始 stderr。验收：失败用例不产生任何错误行号，且 stderr 原样呈现。
- [ ] T19.5 运行 M19 固定质量闸门并创建独立提交。
  验收：gofmt、vet、test、race、diff-check、markdownlint 全部通过。

## 依赖约束

```text
M16（阶段 3）→ M17 → M18 → M19
```

- 不引入共享 shell 内核；每方言独立包，接受 analyzer 逻辑重复。
- 不新增第三方依赖；运行时探测使用标准库 `os/exec`。
- `docs/archive/` 冻结；`IDEA.md` 不修改。
- 方言 e2e 一律以 `exec.LookPath` 守卫，缺方言时 skip，保持 CI 可移植。
