# Macaronic 开发计划（阶段 4）

> 阶段 4 主题：**新增 shell 方言引擎（sh / zsh / csh）与运行时预检**。阶段 3
> （M14–M16，诊断回映、runner 加固、一维数组）已归档于
> `archive/development-plan-phase3.md`。M17、M18、M19 按顺序推进，并分别
> 独立提交。
>
> 测试约定：table-driven 单元测试 + golden/产物断言 + 跨方言端到端。
> 固定质量闸门：`gofmt -l .`（无输出）、`go vet ./...`、`go test ./...`、
> `go test -race ./...`、`git diff --check`、
> `npx --no-install markdownlint-cli2 docs/ examples/`。
>
> **方言能力边界以实测探针为准，不凭语法知识推断。** 探针原始结论记录在各
> 里程碑的「实测事实」小节。

## 背景：实测事实（实现前探针，环境 bash 5.2 / dash / zsh 5.9 / tcsh 6.24.13）

| 方言 | 标量注入 | 一维数组注入 | 报错含 `file:line` |
| --- | --- | --- | --- |
| bash | 可用 | `mapfile -d ''` + `<(...)` | 是（`file: line N:`） |
| sh（dash） | 可用 | **不可能**（无数组语法、无 `<(...)`） | 是（`file: N:`） |
| zsh | 可用 | 需 `while read -d ''` 循环 + `<(...>` | 是（`file:N:`） |
| csh（tcsh） | 可用 | **不可靠**（NUL 流被按空白切分） | **否（无定位信息）** |

关键探针证据：

- `sh`：`a=(1 2 3)` 与 `< <(...)` 均 `Syntax error`；`count=$(...)`、`${name-}`、
  `set -eu` 正常。
- `zsh`：无 `mapfile`；默认 `nomatch` 使未引用的 `[]` 报错，且该失败发生在
  `<(...)` 内时**不影响外层退出码**（数组静默变空，`exit 0`）；`set -u` 下
  引用未定义数组硬报 `parameter not set`（bash 同样输入则展开为 0 参数）。
- `csh`：`nosuchcmd_xyz: Command not found.`、`Too many ('s.`、
  `nope: Undefined variable.` 等 7 类错误均无文件名与行号，`-x` 仅回显命令
  文本，手册亦无行号特性 → **运行时诊断无法回映到 `.mac` 行号**。
- `csh`：`[]` 是 glob 元字符（`values.macint[]`、`int[]` 未引用即
  `No match.` 且不执行命令）；标量注入
  `set name = "`macaronic codec read 'f' 't'`"` 实测可跑通含空格 `str`。
- `tcsh -e`（exit on any error）可恢复「首个失败即停」语义；`bsd-csh` 无 `-e`，
  命令失败后继续执行并以 `exit 0` 结束（会把失败 stage 报成成功）。

## M17 — 运行时预检与 sh 引擎

- **交付物**：
  - `engine` 增加可选接口 `RuntimeChecker { RequiredCommands() []string }`
    （沿用 `DetailedAnalyzer` 可选接口先例，不破坏既有 mock）。
  - `analyze.Analyzer.Run` 每 stage 探测运行时缺失，产出 `SevError` 级 Issue
    （带 stage 与起始行）并跳过该 stage 的后续分析，使 `check` / `build` /
    `run` 一致 fail-fast。
  - 仅 shell 家族实现该接口：bash / sh / zsh / csh。预检命令与该方言
    `RunCommand` 的 `argv[0]` 一致（`bash` / `sh` / `zsh` / **`tcsh`**）。
    python / go 不实现——其端到端测试为「缺运行时则 skip」，加预检会把环境
    受限时的跳过变成硬失败，破坏可移植性。
  - 新增 `#!sh`：POSIX 标量读写注入；**list 契约类型在 check 阶段报 error
    拒绝**（POSIX sh 无数组，不静默不注入）。
- **依赖**：无（阶段 4 首个里程碑）。
- **验证**：预检单元的缺失与存在两条路径；sh 标量跨块 e2e；sh 使用 list 时报
  error 且行号为原始 `.mac` 行号；全量质量闸门通过。
- **完成标准**：缺失方言在 check/build/run 三入口均以非零退出并给出含方言名
  与所查命令的明确错误；sh 标量跨块传递正确；sh 的 list 用法被明确拒绝而非
  静默漏注入。

## M18 — zsh 引擎

- **交付物**：
  - 新增 `#!zsh`，标量与一维数组均支持。
  - 数组 prologue 用 `while IFS= read -r -d ''` 逐元素循环读入并把元素追加
    到同名数组，配 `< <(macaronic codec read-list ...)`（zsh 无 `mapfile`）。
  - 路径与类型参数**必须双引号**——未引用时 `nomatch` 会让 `<(...)` 内的
    glob 失败静默产生空数组且 `exit 0`。
  - 数组 epilogue 前插入 `(( ${+name} )) || name=()`，消除 `set -u` 下
    「只写不读的 list」硬失败，与 bash 的 0 参数行为持平。
  - 诊断正则 `^(?:\./)?([^:]+):(\d+): (.*)$`（zsh 为 `file:N:` 形式）。
- **依赖**：M17（共用预检与 `RuntimeChecker`）。
- **验证**：zsh 标量与四种数组 e2e；`bash → zsh` 含数组的跨方言集成；
  未定义数组与空数组两条边界；全量质量闸门通过。
- **完成标准**：数组在 bash 与 zsh 之间双向传递正确（含带空格 `str[]`）；只写
  不读的数组在 `set -u` 下不失败；标量无回归。

## M19 — csh 引擎

- **交付物**：
  - 新增 `#!csh`，`RunCommand` 为 `tcsh -e run.csh`：`-e` 是恢复「首个失败
    即停」语义的必要条件（`bsd-csh` 无此选项且失败后继续、以 0 退出）。
  - 标量 prologue `set name = "`macaronic codec read 'f' 't'`"`、epilogue
    `macaronic codec write 'f' 't' "$name"`；路径与类型以**单引号**引用以
    规避 `[]` glob。
  - **list 契约类型在 check 阶段报 error 拒绝**（与 sh 同一策略：NUL 流在
    csh 中被按空白切分，`str[]` 含空格会静默错位，宁可拒绝不可静默）。
  - `ParseDiagnostics` 返回空并在注释中说明原因（tcsh 无定位信息）；运行时
    失败经既有 `backmapFailed` 回退为 stage 信息加原始 stderr。
- **依赖**：M18。
- **验证**：csh 标量跨块 e2e；csh 使用 list 时报 error 且行号正确；csh 运行时
  失败降级为 `stage N` + 原始 stderr（不产生错误行号）；全量质量闸门通过。
- **完成标准**：csh 标量传递正确；卡住的命令使 stage 非零退出而非静默成功；
  **静态**诊断行号精确，**运行时**诊断明确呈现为「无行号」而非猜测行号。

## 里程碑依赖

```text
M17 → M18 → M19
```

## 范围外（本阶段不做）

- 递归与嵌套类型；map/对象类型。
- csh 的一维数组跨块传递（探针结论：不可靠）。
- `bash` 之外的数组下标语义归一（zsh 为 1-based，保持语言原生语义并在文档
  说明）。
- 为 python / go 增加运行时预检。
