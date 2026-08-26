# Macaronic 架构设计

> 本文是 macaronic 的规范性设计文档（定案），取代讨论记录
> `discussion-2026-08-25-architecture.md` 作为实现依据。决策的来龙
> 去脉与备选方案见讨论记录；本文只陈述定案。

## 1. 目标、范围、非目标

### 目标

macaronic 是一个类编译的 **CLI 构建工具**：把单个混用多种编程语言
的脚本文件（`.mac`）切分为各语言独立脚本，按头部契约表**自动注入
跨块变量的读写代码**，生成 shell 驱动按序调用各子程序。

### 范围（MVP）

- 块标记 `#!lang`；`#!mac` head 块声明跨块变量契约
  （TOML `[contract]`）
- 基本类型：`int` / `float` / `bool` / `str`
- 首批块语言：`shell` + `python` + `go`
- 传输介质：每变量一个文件（二进制，脚本内自洽）
- 顺序执行（并行推迟）
- **运行环境**：Unix-like OS；`#!shell` 即 Bash；Python 块需
  Python 3 运行时；Go 块需 Go 工具链。Windows 不在范围。

### 非目标

- 运行时反射、外部静态检查器集成（mypy / go types 等）
- 复合类型：struct（用户自行拆分为基本类型变量）、定长数组、
  需序列化的类型（map/dict/对象等）
- 函数/身份/资源句柄等不可字节化的值
- 多文件：`.mac` 不支持 import 其他 `.mac`
- 插件机制：语言引擎内置注册表，不做动态加载
- 常驻解释器 / session / kernel（.NET Interactive 格式）
- **块间**控制流（驱动层不编排块级 if/loop）。块**内**原生 if/loop
  完全允许，macaronic 只是在其处停止类型传播（见 §6 暂停点）

## 2. `.mac` 源文件格式

### 语法

- 文件由若干**块**组成，每块以行首 `#!<lang>` 标记开始，到下一个
  `#!<lang>` 行或文件结束为止。
- 唯一例外是 **head 块**：`#!mac`，内容为 TOML，其中 `[contract]`
  表声明跨块变量契约。head 块**仅一个、必须位于文件最顶部**。
- 其余块的正文是原样代码，逐行保留源文件行号偏移。

### 完整示例

```text
#!mac
[contract]
count = "int"
total = "float"

#!shell
count=$(wc -l < data.txt)

#!python
count: int
total: float = count * 1.5

#!go
println(total)
```

语义：shell 块写 `count` → python 块声明读 `count`、写 `total` →
go 块读 `total` 并打印。各块的读写代码由 macaronic 自动注入，用户
只写业务逻辑。

### 约束

- 契约变量名必须是**所有使用块语言**的合法标识符。
- **遮蔽禁止**：块内引入与契约变量同名的**新局部绑定**
  （Go 的 `:=` / `var` 声明）→ 编译期报错；对契约变量的**普通赋值**
  是 authorized write，不算遮蔽。Python 无局部声明语法，
  以「引用缺注解」兜底（见下）。
- Python 块内契约变量**必须带类型注解**（`count: int`、
  `total: float`）；检测到引用契约变量却缺注解 → **check 阶段报错**
  （不是警告，也不静默不注入）。
- 无类型块（shell）以契约表声明的类型为准读写转换。

## 3. 编译流水线

```text
parse → check → build → run
```

| 阶段 | 输入 | 输出 | 职责 |
| --- | --- | --- | --- |
| parse | `.mac` 源文件 | Program IR | 切块、解析契约、校验 head 块 |
| check | Program IR | 读写/类型、错误列表 | 类型传播、契约比对、依赖检查 |
| build | IR + 分析结果 | `<脚本名>.run/` 产物树 | 落盘+注入读写、run.sh、source-map |
| run | `.mac` 源文件 | 各子程序按序执行 | 每次全量重跑，不用缓存产物 |

四个阶段对应 CLI 四个子命令（见 §9）；`run` 每次全量重跑，
避免编辑后的 `.mac` 执行到陈旧产物。

## 4. 核心 IR

实现语言为 Go。IR 类型统一归属 `internal/ir` 包：

```go
// internal/ir
package ir

type Program struct {
    ScriptName string
    Contract   Contract
    Stages     []Stage
}

type Contract struct {
    Vars map[string]BasicType // 变量名 → 基本类型
}

type BasicType string

const (
    TypeInt   BasicType = "int"
    TypeFloat BasicType = "float"
    TypeBool  BasicType = "bool"
    TypeStr   BasicType = "str"
)

// VarSet 是 ReadSet / WriteSet 的共同类型。
type VarSet map[string]bool

type Stage struct {
    Index     int    // 阶段序号，从 1 起
    Lang      string // shell / python / go
    Source    []string
    StartLine int    // 块首在 .mac 中的行号
    ReadSet   VarSet
    WriteSet  VarSet
}

// OriginKind 区分生成行的来源。
type OriginKind int

const (
    OriginSource   OriginKind = iota // 来自 .mac 源文件
    OriginSynthetic                  // 无源行（注入的读写代码）
)

type SourceSpan struct {
    Start int // .mac 行号（含）
    End   int // .mac 行号（含）
}

type SourceMapEntry struct {
    GeneratedFile string      // 生成文件路径
    GenStart      int         // 生成文件行号范围 [GenStart, GenEnd]
    GenEnd        int
    Span          *SourceSpan // nil 表示合成行
    Origin        OriginKind
    Hash          string      // 生成行内容哈希，用于校验
}

type SourceMap struct {
    Entries []SourceMapEntry
}

// Diagnostic 是引擎从报错文本提取的一条诊断。
type Diagnostic struct {
    File string // 生成文件路径
    Line int    // 生成文件行号
    Msg  string
}
```

分析结果（ReadSet / WriteSet / 类型）由 `check` 阶段写入 `Stage`，
`build` 阶段消费。

`ExecutionPlan` 归属 `internal/plan`：

```go
// internal/plan
type ExecutionPlan struct {
    ScriptName string
    Steps      []Step
}

type Step struct {
    StageIndex int
    Command    []string // 来自 Engine.RunCommand
}
```

## 5. Go 包划分

```text
cmd/macaronic/           CLI 入口，子命令分发
internal/ir/             Program / Contract / Stage / VarSet /
                         SourceMap / Diagnostic
internal/source/         源文件读取、块切分（#!lang）
internal/contract/       TOML 契约解析与校验（返回 ir.Contract）
internal/analyze/        语言无关的推断框架：符号表、类型集合、
                         读写推断、依赖校验
internal/plan/           ExecutionPlan：顺序执行计划（MVP）
internal/codec/          二进制编码（脚本内 ABI，§10）
internal/engine/         Engine 接口 + shell / python / golang
                         实现（含报错解析）
internal/emit/           注入读写、落盘、run.sh、source-map 生成
internal/runner/         子进程调度、退出码、错误回映
internal/sourcemap/      源映射查询（生成行 → .mac 行）
```

`internal/contract` 只负责解析与校验、返回 `ir.Contract`；
`analyze` / `engine` / `plan` 之间只交换 `internal/ir` 类型，
不依赖解析器自有类型。

## 6. 跨块数据流与依赖规则

数据流由契约表驱动：`[contract]` 表是**运行时数据流规范**，
macaronic 按表为每块自动注入读写代码（编译期 codegen，
非运行时反射）。

- **注入接口**：同名变量。prologue 在块开头从 `state/<var>`
  读入赋给同名变量；epilogue 在块末尾把同名变量写回文件。
- **读写集合推断**：块内**引用**的契约变量注入「读」，块内**赋值**
  的注入「写」；推断不出的名字不注入。
- **遮蔽禁止**：见 §2 约束——新局部绑定报错，普通赋值是
  authorized write。
- **类型映射**：契约类型在块语言有对应类型则直接用；无对应类型
  取最接近类型，**check 阶段警告**。
- **依赖检查**：编译期检查「读未写」——某块读取的变量必须由
  更早的块写过，否则报错。无初值机制，第一个写者的赋值创建文件。
- **块序不重排**；并行**推迟**，MVP 只顺序执行。
- **多块写同一变量**：顺序多写允许（后写覆盖）；不允许同时写
  （写-写依赖强制串行）。
- **`run` 在重建后、执行前清空 `state/`**（见 §7），防止上次
  运行残留造成假依赖满足。
- **确定性产物**：契约表按键名排序、产物不写入时间戳，相同输入
  产生稳定产物与 source-map。

### 类型传播种子与暂停点

- **种子**：显式类型声明（`int x;` / `var x int` / `x: int = 0`）、
  字面量赋值（`x = 5` / `5.0` / `"s"` / `True`）、函数返回类型
  签名（`def f() -> int` / `func f() int`）。
- **暂停点**（停止传播、静默通过，宁可漏报不误报）：原生 if/loop
  分支、跨类型运算、无签名或需读函数体的调用。遮蔽不静默——
  命中契约表名字即报错。

## 7. 构建产物布局与排他锁

`macaronic build foo.mac` 在脚本同目录生成：

```text
foo.mac.run/
  stage1/           # 第 1 块：代码 + 注入的读写
  stage2/           # 第 2 块
  state/            # 跨块变量，每变量一个二进制文件
  run.sh            # 驱动：保序调用各 stage
  sourcemap.json    # 生成行 → .mac 行映射
```

锁与生命周期：

- 排他锁为**同目录兄弟文件** `<脚本名>.run.lock`（不放在 `.run/`
  内，避免重建时删除自己的锁）。
- `build`：加锁 → 重建 stage/run.sh/sourcemap 产物 → 释放锁。
  **保留 `state/` 现有内容**（不清空、不覆盖）。
- `run`：加锁 → 重建 → **显式清空 `state/`** → 执行 run.sh →
  释放锁（**单一锁**，不嵌套调用 build 的内部锁）。
- 锁被占用时 fail-fast 报错退出。

run.sh 失败报告：

- 每个 stage 的 stderr 捕获到 `stageN/stderr.txt`。
- 失败清单 `<脚本名>.run/failure.json`，字段：
  `stage_index`、`exit_code`、`stderr_path`。某 stage 非零退出时
  由 run.sh 写出并立即退出；每次 run 开始前删除该文件。
- runner 读失败清单确定失败 stage，选对应 Engine 调
  `ParseDiagnostics`，再经 source-map 回映。

## 8. Engine 接口与职责

```go
type Engine interface {
    Name() string // shell / python / go

    // Analyze 做块内类型传播，返回本块读/写的契约变量集合。
    Analyze(st *ir.Stage, c ir.Contract) (readSet, writeSet ir.VarSet,
        err error)

    // Emit 生成块文件（含注入的读写）到 stageDir，并向 source-map
    // 记录条目。
    Emit(st *ir.Stage, c ir.Contract, stageDir, stateDir string,
        sm *ir.SourceMap) error

    // RunCommand 返回 run.sh 中调用本块的命令。
    RunCommand(stageDir string) []string

    // ParseDiagnostics 从本语言报错文本提取（生成文件, 行号, 消息）
    // 列表，供 runner 经 source-map 回映到 .mac 行号。
    ParseDiagnostics(stderr []byte) []ir.Diagnostic
}
```

各语言职责：

- **shell**：无类型，以契约类型为准读写转换。Bash 不能安全持有
  任意二进制（含 NUL 字节），因此 shell 块注入的读写**调用
  codec helper**（§10），不承诺纯 Bash 直接解析二进制。
- **python**：契约变量须带类型注解（缺注解 = check 报错）；
  prologue 读文件赋值、epilogue 写回文件，读写走 codec。
- **go**：用户块是**纯语句**（statement-only）。engine 负责
  `package main` + `func main` 包裹与类型声明，块内容放入 main 体；
  含 `go build` 步骤。生成 wrapper **只 import macaronic 的
  runtime/codec 依赖**；用户自定义 import **推迟**（不推断 import）。

## 9. 命令行界面

```text
macaronic <parse|check|build|run> <script>
macaronic <script>   # 等价于 macaronic run <script>
```

- `parse`：源文件 → 块列表 + 契约（打印 IR 摘要）
- `check`：契约比对 / 类型传播 / 依赖检查（只检查，不生成）
- `build`：块落盘 + 注入读写 + run.sh + source-map
- `run`：**每次**执行 `parse → check → build → 清空 state → 执行 run.sh`（确定性全量重跑）

## 10. 二进制 codec

- codec 是**构建内 ABI**：只保证同一脚本内读写双方一致（读写
  双方均为 macaronic 生成），**无跨脚本、跨版本兼容承诺**。
- 单一 codec 计划驱动所有引擎：所有引擎读写的 `state/<var>`
  均按同一布局编码。
- **MVP 定案布局**：
  - `int`：8 字节 little-endian 补码（int64）
  - `float`：8 字节 IEEE-754（float64）
  - `bool`：1 字节（0 / 1）
  - `str`：4 字节 little-endian 长度 + UTF-8 字节
- **codec helper**：shell 块的注入读写不直接解析二进制，而调用
  macaronic 的隐藏子命令 `codec`：
  - `macaronic codec read <state-file> <type>` →
    输出文本值（stdout）
  - `macaronic codec write <state-file> <type> <value>` →
    写二进制
    生成的 shell prologue/epilogue 经 `$(macaronic codec read ...)` /
    `macaronic codec write ...` 读写；Python/Go 引擎直接内嵌同一
    codec。

## 11. 错误模型与源映射

- **source-map**：以「生成文件路径 + 行号/范围」为主键、内容哈希
  为校验值。行哈希**不能单独反查重复行**（同一内容可能多处出现），
  只作一致性校验。
- **合成行**：注入的 prologue/epilogue 以 `OriginSynthetic` 记录，
  **无源行号**；报错落在合成行时按内部错误报告（不应发生）。
- **报错解析**：runner 不通用解析 stderr，而是调用失败块 engine 的
  `ParseDiagnostics`，分别处理 Bash 错误、Python traceback、
  Go 编译错误与 Go 运行时栈。
- **失败即停**：任一块非零退出，立即停止，不继续后续块。
- **错误回映**：块失败时，用 `ParseDiagnostics` + source-map 把
  生成文件的报错行映射回 `.mac` 源文件行号，报告给用户；不直接
  暴露 `stageN/` 下的生成文件。
- **现场保留**：`state/` 保留现场供排查，下次运行前清空。

## 12. 安全边界与已知限制

- **防滑线**：类型由块代码各自的语言定义，macaronic 只按契约表
  搬运值（文件读写），不判语义；控制流留在 shell 驱动。
- **硬边界**：块间不共享函数/import——每块独立进程，只经
  `state/` 传数据，不传代码。
- **已知限制**：
  - 推断失败则不注入，可能导致运行时错误（未定义名）；
    macaronic 的检查是「轻量静态检查」，**不承诺完全编译期安全**。
  - shell 块的二进制处理依赖 `macaronic codec` helper，纯 Bash
    表达力有限。
  - 固定 `<脚本名>.run/` 目录存在并发运行竞争，以 fail-fast
    排他锁规避。
  - struct 等复合类型需用户手工拆分为基本类型变量。
