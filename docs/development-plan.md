# Macaronic 开发计划（阶段 3）

> 阶段 3 主题：**诊断可用性、运行可靠性与基础复合类型**。阶段 2
>（M11–M13，推断/诊断增强）已归档于 `archive/development-plan-phase2.md`。
> M14、M15、M16 按顺序推进，并分别独立提交。
>
> 测试约定：table-driven 单元测试 + golden/产物断言 + shell→python→go
> 端到端。固定质量闸门：`gofmt -l .`（无输出）、`go vet ./...`、
> `go test ./...`、`go test -race ./...`、`git diff --check`、
> `npx --no-install markdownlint-cli2 docs/ examples/`。

## M14 — 静态诊断回映到原始源码

- **交付物**：
  - 扩展分析结果，使 engine 能返回已知读/写变量的源码 span 和结构化
    `ir.Diagnostic`；保留现有读写集合、warning/error 语义和排序。
  - `Analyzer.Run` 将 stage body 的相对行号统一转换为原始 `.mac` 行号，
    不使用生成文件 sourcemap 处理静态诊断。
  - 读未写错误使用实际读引用位置；遮蔽、缺注解和引擎诊断使用其诊断 span；
    无法确定位置时保持现有 stage 起始行回退。
  - CLI 输出包含准确的 stage、原始 line 和变量信息。
- **依赖**：M13。
- **验证**：非首行失败构造的 table-driven/golden 测试；三引擎诊断测试；
  CLI 断言准确原始行号、stage 和变量；全量质量闸门通过。
- **完成标准**：静态诊断不再全部指向 stage 首行；未知 span 宁可回退，
  不产生不可信的行号；现有 warning/error 行为不回归。

## M15 — 现有 runner 串行执行加固

- **交付物**：
  - 保持 `runner.Run` 同步 API、保序执行、首个失败即停和无 timeout 语义。
  - 明确并覆盖退出码（包括命令不存在）、combined output、失败 stage、
    stage 目录对齐和后续 stage 不执行。
  - 失败时可靠保留对应 `failure.stderr`；写入失败通过现有结果/错误路径
    暴露，不吞掉原始进程失败。
  - CLI 端到端验证 `failure.json`、失败回映、warning-only 仍可 build/run、
    static error 阻止执行。
- **依赖**：M14；不引入 context、超时、重试、并发或进程树清理。
- **验证**：runner 单元测试、真实 `check → build → run` fixtures、
  失败现场与退出码断言、全量质量闸门通过。
- **完成标准**：首个失败的 stage、退出码、stderr、failure.json 和回映结果
  一致；不执行后续 stage；既有成功路径保持不变。

## M16 — 基础类型一维数组跨块传递

- **交付物**：
  - 契约支持 `int[]`、`float[]`、`bool[]`、`string[]` 四种一维 homogeneous
    list；拒绝嵌套列表、对象、联合类型、nullable 和混合元素。
  - 保持四种标量 wire format 不变；数组格式为 little-endian `uint32`
    元素数量，后接逐元素标量编码；解码前限制元素数量，避免无界分配。
  - codec 显式支持 `[]int64`、`[]float64`、`[]bool`、`[]string`，不以
    reflection 或 `[]any` 作为公共行为；字符串中的 NUL 明确拒绝。
  - 三引擎生成数组读写 plumbing；至少一个 shell→python→go 跨引擎 E2E
    覆盖写入、读取/修改、再写入和最终 codec 值。
- **依赖**：M15；仅在 M14/M15 稳定后接入。
- **验证**：四种数组 round-trip、损坏数据和超大数量测试；contract 语法
  测试；Python/Shell/Go 产物 prologue/epilogue 断言；跨引擎 E2E；全量
  质量闸门通过。
- **完成标准**：数组可在三引擎间安全传递；标量兼容性不变；非法/过大数据
  明确失败；不扩展为递归类型系统。

## 里程碑依赖

```text
M13（阶段 2）→ M14 → M15 → M16
```
