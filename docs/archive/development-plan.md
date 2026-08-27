# Macaronic 开发计划

> 对应 `architecture.md` 的 10 个可验证里程碑。每个里程碑列交付物、
> 依赖、验证命令与完成标准。
> 测试约定：table-driven 单元测试 + golden tests + shell→python→go
> 端到端（E2E）。
> **固定质量闸门**（每个里程碑默认全跑）：`gofmt -l .`（无输出）、
> `go vet ./...`、`go test ./...`；涉及锁/并发/子进程的里程碑加
> `go test -race ./...`。下方「验证」只列里程碑专属检查。

## M1 — Go module 与 CLI 骨架

- **交付物**：`go.mod`；`cmd/macaronic/main.go`；子命令分发
  （`parse` / `check` / `build` / `run`）+ `macaronic <script>`
  简写为 `run`；基础错误退出码。
- **依赖**：无。
- **验证**：`go build ./...`；`go test ./...`；
  `go run ./cmd/macaronic --help`。
- **完成标准**：四子命令可解析；未知命令报错并退出非零；
  `macaronic foo.mac` 等价 `run`。

## M2 — `#!lang` 切块、TOML 契约与 IR

- **交付物**：
  - `internal/ir`（`Program` / `Contract` / `Stage` / `VarSet` /
    `SourceMap` / `Diagnostic`）
  - `internal/source`（块切分）
  - `internal/contract`（TOML `[contract]` 解析与校验，返回
    `ir.Contract`；固定 TOML 库 `github.com/BurntSushi/toml`，
    **在 go.mod 固定精确版本，不用 latest**）
- **依赖**：M1。
- **验证**：table-driven 切块测试 + golden tests（head 缺失、
  head 非置顶、多 head、非法变量名）；确定性输出（契约键排序）。
- **完成标准**：正确切分 head + 多块并保留行号偏移；
  契约解析与校验报错准确；相同输入产生稳定 IR。

## M3 — 语言无关推断框架与依赖检查

- **交付物**：`internal/analyze` 的**语言无关**部分：符号表机制、
  类型集合（`VarSet`）、读写推断公共框架、依赖校验器（读未写、
  遮蔽规则接口）。**各语言 Analyze 实现归 M6–M8**。
- **依赖**：M2。
- **验证**：依赖校验器 table-driven（读未写报错、后写覆盖通过、
  多块写不冲突）；确定性 golden 输出。
- **完成标准**：语言无关框架可被 shell/python/go 引擎复用；
  依赖检查报错带块号与变量名。

## M4 — 产物目录、排他锁与 source map

- **交付物**：`internal/emit` 的目录创建与 state 清空逻辑
  （state 清空仅由 `run` 流程调用，`build` 不清空）；fail-fast
  排他锁（`build` / `run` 共用，`run` 全程持锁）；
  `internal/sourcemap`（生成路径 + 行号为主键、内容哈希为校验、
  `OriginKind` 区分源行/合成行）。
- **依赖**：M2。
- **验证**：并发运行同一脚本的锁测试；sourcemap 生成/查询测试
  （含重复行、`OriginSynthetic` 合成行）。
- **完成标准**：`<脚本名>.run/` 结构正确；`build` 保留 `state/`
  现有内容；锁生效；sourcemap 可反查任意生成行。

## M5 — 构建内二进制 codec 与 codec helper

- **交付物**：`internal/codec`（int64 / float64 / bool / str 定案
  布局）；`macaronic codec read/write` 隐藏子命令。
- **依赖**：M1。
- **验证**：round-trip table-driven 测试（边界值：0、负值、
  最大值、空串、多字节 UTF-8）；codec 子命令 CLI 测试。
- **完成标准**：四类型编解码 round-trip 无损；同一 codec 被所有
  引擎与 codec helper 共用。

## M6 — shell engine

- **交付物**：`internal/engine/shell`：`Analyze`（无类型，以契约
  类型为准）、`Emit`（注入读写经 codec helper）、`RunCommand`、
  `ParseDiagnostics`（Bash 报错行解析）。
- **依赖**：M3、M4、M5。
- **验证**：shell→shell 变量传递 E2E（str/int/float/bool）；
  Bash 报错诊断解析测试；sourcemap 条目生成。
- **完成标准**：shell 块正确读写契约变量（经 codec helper，
  不依赖纯 Bash 解析 NUL）；报错行可回映。

## M7 — python engine

- **交付物**：`internal/engine/python`：`Analyze`（契约变量**必须**
  类型注解，缺注解 = check 报错）、`Emit`（prologue 读 /
  epilogue 写）、`RunCommand`、`ParseDiagnostics`（traceback
  解析）。
- **依赖**：M3、M4、M5。
- **验证**：python→python、shell→python E2E；缺注解报错测试；
  traceback 诊断解析测试；sourcemap 条目生成。
- **完成标准**：python 块类型注解推断正确；缺注解报错；读写注入
  正确；与 shell 传递的类型一致。

## M8 — go engine

- **交付物**：`internal/engine/golang`：`package main` + `import` +
  `func main` 包裹、类型声明、`go build`、`RunCommand`、
  `ParseDiagnostics`（编译错误 + 运行时栈）。
- **依赖**：M3、M4、M5。
- **验证**：go 块 E2E（读入前块变量、写出）；编译错误/运行时栈
  诊断解析测试；sourcemap 条目生成。
- **完成标准**：go 块编译运行正确；main 包裹对用户透明；
  报错行回映正确。

## M9 — build/run 驱动与错误回映

- **交付物**：`internal/runner`：保序调度、失败即停、退出码；
  错误回映（`ParseDiagnostics` + sourcemap → `.mac` 行号）；
  失败清单 `failure.json` 写出/清理；`run` 每次全量重跑。
- **依赖**：M4、M6、M7、M8。
- **验证**：失败注入测试（某块非零退出）；报错行号映射测试；
  stale-source 测试（编辑 `.mac` 后 `run` 不执行陈旧产物）。
- **完成标准**：任一块失败即停且不暴露 `stageN/` 生成文件；
  错误行号准确回映；失败清单 `failure.json` 正确写出并在下次
  run 前删除；`run` 在重建后清空 `state/`。

## M10 — 端到端示例、异常路径与文档收尾

- **交付物**：`examples/` 下 shell→python→go 完整示例；异常路径
  用例（读未写、类型冲突、遮蔽、块失败）；`README.md`（安装、
  用法、示例）。
- **依赖**：M9。
- **验证**：`go test ./...` 全绿；示例端到端跑通；异常用例按预期
  报错；`git diff --check` 干净。
- **完成标准**：示例可复现运行；文档与 `architecture.md`、
  `IDEA.md` 一致。

## 里程碑依赖

```text
M1 → M2, M5
M2 → M3, M4
M3, M4, M5 → M6, M7, M8
M4, M6, M7, M8 → M9
M9 → M10
```
