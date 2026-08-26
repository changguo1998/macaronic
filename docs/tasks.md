# Macaronic 里程碑任务清单

> 本文件是根据 [`docs/development-plan.md`](development-plan.md) 拆解的
> 执行级任务清单，每个里程碑定义、依赖、交付物、验证原则均以开发
> 计划为准。每个任务（T-ID）粒度约 0.5–2 工时。

## 任务维护约定

- 每个 T-ID 对应一个具体的实现/测试动作，完成后把 `[ ]` 改为 `[x]`。
- 每个 T 完成的标准是它自带的「验收」句；里程碑完成的标准是该节
  T 全部打勾。
- 开发计划中的**固定质量闸门**在每个里程碑都需通过：
  1. `gofmt -l .` 无输出
  2. `go vet ./...` 无输出
  3. `go test ./...` 全绿
  4. M4、M6–M9 额外加 `go test -race ./...` 全绿

## 里程碑依赖（同 development-plan.md）

```text
M1 → M2, M5
M2 → M3, M4
M3, M4, M5 → M6, M7, M8
M4, M6, M7, M8 → M9
M9 → M10
```

## 进度总览

| 里程碑 | 预估工作量 | 已打勾 / 总数 |
| --- | --- | --- |
| M1 | 2–3 工时 | 0 / 7 |
| M2 | 8–12 工时 | 0 / 8 |
| M3 | 4–6 工时 | 0 / 7 |
| M4 | 6–9 工时 | 0 / 6 |
| M5 | 3–4 工时 | 0 / 5 |
| M6 | 6–9 工时 | 0 / 6 |
| M7 | 8–12 工时 | 0 / 7 |
| M8 | 8–12 工时 | 0 / 8 |
| M9 | 10–14 工时 | 0 / 6 |
| M10 | 4–6 工时 | 0 / 4 |
| **M1–M10** | **约 60–90 工时** | **0 / 64** |

---

## M1 — Go module 与 CLI 骨架

- [x] T1.1 写 `go.mod`：module `github.com/changguo1998/macaronic`
  （对齐远程仓库）、Go 1.22。
- [x] T1.2 写 `cmd/macaronic/main.go`：入口只分发，逻辑进
  `internal/cli`。
- [x] T1.3 `internal/cli` 子命令 `parse|check|build|run`；未知子命令
  报错、退出非零。
- [x] T1.4 简写：`macaronic <script>` 等价于 `run`；多余位置参数
  退出非零并给出用法提示。
- [x] T1.5 `--help` 与子命令级帮助文本，列出全部子命令。
- [x] T1.6 四个子命令先行 stub（显示未实现），后续 M2、M3、M4、M9
  逐步替换。
- [x] T1.7 冒烟测试：子命令/简写/未知命令/多余参数的退出码断言。

## M2 — `#!lang` 切块、TOML 契约与 IR

- [x] T2.1 `internal/ir`：`Program / Contract / Stage / VarSet` 与
  `BasicType` 四个类型常量；`VarSet` 为 `map[string]bool`。
- [x] T2.2 `internal/ir`：`SourceSpan / SourceMapEntry / SourceMap /
  Diagnostic` 与 `OriginKind`；`Span == nil` 表示合成行。
- [x] T2.3 `internal/source`：切块算法（逐行扫描 `#!` 前缀，记录
  块开始行号，块止于下一块或 EOF，含空块与块尾无终止符的正确
  处理）。
- [x] T2.4 head 块规则：`#!mac` 仅一个、必须位于文件最顶部，违规
  报错（表驱动覆盖 head 缺失/非置顶/多 head 场景）。
- [x] T2.5 `internal/contract`：使用 `github.com/BurntSushi/toml`
  解析（go.mod 固定精确版本），提取 `[contract]` 表、键→
  `BasicType` 映射。
- [x] T2.6 契约校验：非法变量名、重复键、值非法（golden 表驱动，
  非法类型/缺失类型/契约名不符命名规则全场景）。
- [x] T2.7 `internal/contract` 导出：`Parse(head []string)` 只输出
  `ir` 类型；键序只在打印/序列化/产物生成时排序（Go map 无稳定
  键序）。
- [x] T2.8 M2 集成验收：切块、契约、确定性输出回归自测脚本。

## M3 — 语言无关推断框架与依赖检查

- [x] T3.1 `internal/analyze`：符号表机制（名字→类型），顺序求值、
  后写覆盖（含架构 §6 暂停点定义）。依赖 M2。
- [x] T3.2 `type EngineAdapter` 骨架接口 + 语言无关读写推断公共
  实现，语言差异由引擎适配器（mock）隔开。依赖 M2。
- [x] T3.3 依赖校验器：`read-before-write` 报错；后写覆盖通过；
  多块写不冲突。错误信息输出「块号 + 变量名 + 类型」。依赖 M2。
- [x] T3.4 遮蔽上报钩子（语言无关）：引擎上报「新局部绑定」
  检测结果，框架负责报错，不静默。「检测」归引擎 Analyze
  实现（见 T6.1 / T7.1 / T8.8）。依赖 T3.1。
- [x] T3.5 遮蔽上报的 mock 演示：引擎适配器上报、框架报错。
  依赖 T3.1。
- [x] T3.6 检查报告结构：错误按块号/变量/行号组织；0 错误样例
  演示。
- [x] T3.7 `check` 命令接入推断框架（打印检查报告）；Mock 引擎。
  验收：`go test ./internal/analyze/...`。依赖 M2。

## M4 — 产物目录、排他锁与 source map

- [x] T4.1 目录结构创建（`<脚本名>.run/` 下 `stageN/`、`state/`、
  `run.sh`、`sourcemap.json`）与清空函数。
- [x] T4.2 `internal/sourcemap`：记录接口（`AddEntry`）与查询
  （`Resolve(genFile, genLine)` 到源行映射），重复行不合并。
- [x] T4.3 跨进程排他锁（stdlib `syscall.Flock`），`Lock`/`Unlock`
  成对，锁文件为 `<脚本名>.run.lock` 兄弟文件。fail-fast 抢占即时报错。
- [x] T4.4 sourcemap.json 序列化 + 内容哈希校验实现。
- [x] T4.5 `build` 与 `run` 共用创建逻辑：`build` 保留 `state/`
  现有内容，`run` 清 `state/`；run.sh 生成（保序调用）。
  依赖 M2。
- [x] T4.6 并发锁竞态测试（`go test -race`）。

## M5 — 构建内二进制 codec 与 codec helper

- [x] T5.1 `internal/codec`：int64/float64/bool/str 编解码、
  round-trip 边界值。
- [x] T5.2 `macaronic codec read <state-file> <type>`：按类型读
  文件、输出文本值到 stdout。
- [x] T5.3 `macaronic codec write <state-file> <type> <value>`：
  按位置取值写二进制文件。
- [x] T5.4 CLI 错误路径（缺参数/错类型报错、退出非零）。
- [x] T5.5 codec 一致性、常量输出比对测试（引擎共享）。

## M6 — shell engine

- [ ] T6.1 `Analyze`：推断 shell 读（`$name` / `${name}`）、写
  （`name=...`）；转换由契约表类型驱动。
- [ ] T6.2 `Emit`：注入 prologue/epilogue 读写，调用 codec helper，
  不依赖纯 Bash 解析 NUL。依赖 M3、M4、M5。
- [ ] T6.3 `RunCommand`（`bash <genfile>`）+
  `ParseDiagnostics`（`script.sh: line N: ...` 诊断）。依赖 M3、M4、M5。
- [ ] T6.4 shell 转换函数（str/int/float/bool ↔ codec helper）。
- [ ] T6.5 shell 引擎整体 E2E：shell→shell 变量传递
  （str/int/float/bool）。
- [ ] T6.6 sourcemap 条目生成与抽查（重复行、`OriginSynthetic`）。

## M7 — python engine

- [ ] T7.1 `Analyze`：Python 块契约变量必须带类型注解（`x: int`），
  缺注解报错。依赖 M3、M4、M5。
- [ ] T7.2 「契约变量必注解」错误场景回归测试（错误文本+行）。
- [ ] T7.3 `Emit`：prologue（读文件赋变量）+ epilogue（写回文件），
  按契约类型转换。
- [ ] T7.4 `RunCommand`（`python3 <genfile>`）+ `ParseDiagnostics`
  解析 Python traceback（生成文件、行号、消息）。
- [ ] T7.5 python→python E2E 与 shell→python 传递类型一致性测试。
- [ ] T7.6 sourcemap 条目生成与抽查。
- [ ] T7.7 python 引擎整体 E2E（读入前块变量、写出、必注解
  check 报错全链路）。

## M8 — go engine

- [ ] T8.1 `internal/engine/golang`：`func main` 包裹用户代码；import 形态按 T8.3。
  依赖 T8.3。用户 import 暂不支持。
- [ ] T8.2 契约变量 Go 类型声明 + prologue/epilogue Go 生成。依赖 T8.3。
- [ ] T8.3 codec 访问定案：生成 stage 无法 import `internal/codec`
  （internal 包规则）——定案为注入自包含 codec 代码或建立公共
  runtime 包。
- [ ] T8.4 `go build` 编译错误捕获；`RunCommand` 执行生成的
  二进制（非 `go run`）。
- [ ] T8.5 `ParseDiagnostics`（go 编译错误 + 运行时栈两模式）。
- [ ] T8.6 sourcemap 条目生成与抽查。
- [ ] T8.7 go E2E：读入前块变量、写出、报错行回映。
- [ ] T8.8 `Analyze`：推断 go 块读写（契约变量声明/引用；`:=`
  新绑定即遮蔽报错）。

## M9 — build/run 驱动与错误回映

- [ ] T9.1 `internal/runner` 保序调度多个 stage，失败即停、
  保持非零退出。
- [ ] T9.2 退出码汇总，含普通退出码传递与失败信号。
- [ ] T9.3 错误回映：failed stage engine 解析段 + sourcemap 映射
  行号。
- [ ] T9.4 run 每次全量重跑（parse→check→build→清 state→执行
  run.sh）。
- [ ] T9.5 failure.json 由 run.sh 写并立即退出；下次 run 开始时
  删除该文件，保留现场供排查。
- [ ] T9.6 stale-source：编辑 `.mac` 后 `run` 不执行陈旧产物。

## M10 — 示例、异常路径、文档收尾

- [ ] T10.1 `examples/`：shell→python→go 完整示例（含 `README`）。
- [ ] T10.2 异常路径用例：读未写、类型冲突、遮蔽、块失败。
- [ ] T10.3 `README.md`：安装、用法、示例；文档与 architecture.md、
  IDEA.md 一致。
- [ ] T10.4 `go test ./...` 全绿 + `git diff --check` 干净。

## 依赖约束

> 新增依赖：`github.com/BurntSushi/toml`@精确版本。锁用 stdlib
> `syscall.Flock`（见 T4.3），codec 自研。其他库不新增。

## 执行阶段划分与并行分工

| Phase | 内容 | 并行 | 落点分支 |
| --- | --- | --- | --- |
| A | M2 → M3 → M4 → M5 | 无（顺序） | master |
| B | M6 shell / M7 python / M8 go | 3 worktree 并行 | 各 engine 分支 |
| C | 三支 cherry-pick + 注册接线 + 跨语言 E2E | 无（顺序） | master |
| D | M9 | 无 | master |
| E | M10 | 无 | master |

约定：

- A 阶段冻结共享接口（`ir`、engine 接口、codec、sourcemap、目录
  约定）；T8.3 定案：go 块生成自包含 codec 代码，与
  `internal/codec` 交叉对照验证。
- B 阶段每个 lane 只允许改 `internal/engine/<lang>/**`，禁止改：
  `go.mod`、`go.sum`、`internal/cli`、共享 engine 接口/registry、
  `docs/` 与示例文件。接口不满足时禁止自行改接口，停止并上报。
- B 阶段每个 lane 的验收先本地通过：`gofmt -l .`、`go vet ./...`、
  `go test ./...`、`go test -race ./...`，然后分支提交，报告 commit
  hash 与改动文件清单。
- C 阶段三支 cherry-pick 后，父（master）负责引擎注册、CLI 接线与
  跨语言 E2E，跑完整质量闸门。
- D、E 顺序执行。
