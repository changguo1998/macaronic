# Macaronic 开发计划（阶段 6）

> 阶段 6 主题：**新增 node 引擎（`#!node`）**。阶段 5（M20 可靠性补齐、
> M21 持续集成已取消）已归档于 `archive/development-plan-phase5.md`。
>
> 测试约定：table-driven 单元测试 + 生成文本断言 + 跨引擎端到端。
> 固定质量闸门：`gofmt -l .`（无输出）、`go vet ./...`、`go test ./...`、
> `go test -race ./...`、`git diff --check`、`markdownlint-cli2`
> （markdownlint 由开发环境提供）。
>
> **目标语言行为以实测探针为准，不凭语法知识推断。** 探针结论记录在下面的
> 「实测事实」小节。

## 背景：实测事实（实现前探针，Node.js v24.16.0 / Linux x86-64）

| 场景 | 实测结果 |
| --- | --- |
| 抛错 | 退出码 1；stderr 首行 `<绝对路径>:2`，栈帧 `:2:9` |
| 语法错误 | 同样给出 `<绝对路径>:1` 与 `^` 标记 |
| `execSync('false')` | 抛错且 `e.status = 1`，可用于「失败即停」 |
| `.js` 在 `"type": "module"` 目录 | 按 ESM 解析，`require` 不可用 |
| `.cjs` 位于同一目录 | 正常执行 |
| `Buffer` 读写 int64 / float64 / 长度前缀 UTF-8 | 全部往返正确 |
| `Number(9007199254740993n)` | 得 `9007199254740992`，超过 2^53 丢精度 |
| `BigInt(1.5)` | `RangeError: ... is not an integer` |
| `typeof 未声明变量` | 返回 `'undefined'`，不抛错 |
| 读取不存在的 state 文件 | `readFileSync` 抛 `ENOENT`（`e.code`） |
| 含 NUL 的字符串 | Buffer 原生支持，但与 CLI codec 的 `str` 规则不一致 |

两条结论决定实现：**产物必须是 `.cjs`**（否则用户工程根目录的
`package.json` 带 `"type": "module"` 时会整体失败），**`int` 以
`number` 呈现、经 BigInt 中转读写**（2^53 以上丢精度，记入 §12 限制）。

## M22 — node 引擎

- **交付物**：
  - 新增 `#!node`，生成 `run.cjs`，`RunCommand` 为 `["node", run.cjs]`。
  - **内嵌 codec**：生成的 JS 直接用 `Buffer` 读写 state 文件（不调用
    `macaronic codec`），布局与 §10 完全一致 —— int64 LE、float64 LE、
    bool 1 字节、`str` 4 字节长度 + UTF-8、list 4 字节计数 + 元素编码。
  - **类型映射**：`int` → JS `number`（读写经 `BigInt` 中转，写入非整数
    报含变量名的错误）；`float` → `number`；`bool` → `boolean`；
    `str` → `string`；四种一维数组 → `Array`。
  - **失败响亮**（沿用 M20 文案）：缺 state 文件（`ENOENT`）与未赋值标量
    （`typeof x === 'undefined'`）都输出
    `macaronic: stage N: ...` 并 `process.exit(1)`。
  - **`str` 与 `str[]` 元素写入拒绝 NUL**：与 CLI codec 的规则一致，避免
    同一份 state 在 bash 块里被截断。
  - **分析**：普通赋值 = 写；`x++` / `x +=` = 读 + 写；其余出现 = 读；
    `let` / `const` / `var` 声明命中契约名 = 遮蔽错误。动态语言不需要类型
    注解，类型以契约为准（与 shell 方言同口径）。
  - `RuntimeChecker` 返回 `["node"]`。
  - 文档：§1 能力矩阵加一行、§8 引擎职责、§12 已知限制（2^53 精度、
    CommonJS 而非 ESM、用户块内不能用 `import`）。
- **依赖**：M20（共享失败文案与预检口径）。
- **验证**：分析用例（读写/遮蔽/算术自增）；生成文本断言（`.cjs`、
  codec 内嵌、守卫文案）；与 bash、python 的双向跨引擎 e2e（同一
  `state/` 互通，含 `str[]` 带空格元素）；缺 `node` 时用例 skip 而非失败；
  三个既有示例行为不变；全量质量闸门通过。
- **完成标准**：`#!node` 块能与现有六种块互相传值（标量与四种一维数组）；
  声明写而未赋值、或缺 state 文件时，stage 以非零退出并给出共享文案；
  用户工程根目录有 `"type": "module"` 时仍可运行。

## 里程碑依赖

```text
M20（阶段 5）→ M22
```

## 范围外（本阶段不做）

- ESM / `import` 支持（用户块内可用 `await import()` 自行绕过）。
- TypeScript、异步顶层 `await`、BigInt 直通类型。
- 允许 `str` 含 NUL（与 CLI codec 规则冲突）。
- 其它候选语言（ruby / perl / lua）：本机虽已安装 ruby 3.3.8 与
  perl 5.40.1，但一次只加一种，先把 node 打磨到与现有引擎同水准。
