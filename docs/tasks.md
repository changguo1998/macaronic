# Macaronic 里程碑任务清单（阶段 3）

> 本文件对应 [`development-plan.md`](development-plan.md)，拆解 M14–M16
> 为执行级任务。阶段 2（M11–M13）已归档于
> `archive/tasks-phase2.md`。每个 T-ID 完成后将 `[ ]` 改为 `[x]`。

## 进度总览

| 里程碑 | 预估工作量 | 已打勾 / 总数 |
| --- | --- | --- |
| M14 | 4–6 工时 | 6 / 6 |
| M15 | 4–6 工时 | 6 / 6 |
| M16 | 10–16 工时 | 9 / 9 |
| **M14–M16** | **约 18–28 工时** | **21 / 21** |

## M14 — 静态诊断回映到原始源码

- [x] T14.1 扩展 `ir`/engine 分析结果，携带读写变量的首个已知源码
  span 与结构化诊断；保留现有接口语义或提供最小兼容适配。
  验收：三引擎单元测试能返回 span/diagnostic。
- [x] T14.2 在 `Analyzer.Run` 统一完成 stage body 相对行号到原始 `.mac`
  行号的转换；禁止静态诊断借用生成文件 sourcemap。
  验收：非首行诊断映射到正确原始行。
- [x] T14.3 读未写错误使用实际读引用 span；遮蔽、缺注解和引擎错误使用
  diagnostic span；未知 span 回退到 stage 起始行。
  验收：table-driven 覆盖三种位置来源和回退。
- [x] T14.4 更新 CLI 报告，使 stage、原始 line、变量和消息保持确定性。
  验收：CLI golden 断言精确输出。
- [x] T14.5 补充 Python/Shell/Go 非首行静态诊断回归测试。
  验收：三引擎相关测试通过。
- [x] T14.6 运行 M14 固定质量闸门并创建独立提交。
  验收：gofmt、vet、test、race、diff-check、markdownlint 全部通过。

## M15 — 现有 runner 串行执行加固

- [x] T15.1 覆盖保序执行、首个失败即停、后续 stage 不执行及 stage 目录
  对齐。验收：runner 单元测试含 sentinel 断言。
- [x] T15.2 覆盖正常退出码、命令不存在和失败信号等退出码路径。
  验收：Result/CLI 返回值与约定一致。
- [x] T15.3 覆盖 combined output、stdout 回调和对应 stage 的
  `failure.stderr` 内容/路径。验收：字节级断言通过。
- [x] T15.4 处理 failure.stderr 写入失败：保留原始进程失败，同时通过
  现有错误结果暴露现场写入失败。验收：不可写目录测试。
- [x] T15.5 补充真实 `check → build → run` CLI fixtures，验证
  `failure.json`、失败回映、warning-only 可执行、static error 阻止运行。
- [x] T15.6 运行 M15 固定质量闸门并创建独立提交。
  验收：gofmt、vet、test、race、diff-check、markdownlint 全部通过。

## M16 — 基础类型一维数组跨块传递

- [x] T16.1 扩展 contract/type 表示，支持且仅支持 `int[]`、`float[]`、
  `bool[]`、`str[]`；`string[]` 作为兼容别名规范化为 `str[]`；拒绝嵌套、
  对象、联合、nullable 和混合类型。
- [x] T16.2 定义数组 wire format：little-endian `uint32` 数量 + 标量元素；
  标量格式字节级兼容；解码前限制数量并拒绝损坏数据。
- [x] T16.3 codec 显式支持 `[]int64`、`[]float64`、`[]bool`、`[]string`，
  拒绝嵌入 NUL；不引入 reflection 或 `[]any` 公共路径。
- [x] T16.4 增加四种数组的 round-trip、边界、损坏数据和超大数量测试。
- [x] T16.5 生成 Go 数组读写 plumbing，并添加 prologue/epilogue 产物断言。
- [x] T16.6 生成 Python 数组读写 plumbing，并添加 prologue/epilogue 产物断言。
- [x] T16.7 生成 Shell 数组读写 plumbing，使用 binary-safe bridge，并添加
  产物断言。
- [x] T16.8 增加 shell→python→go 跨引擎 E2E，验证写入、读取/修改、再写入
  及最终 codec 值；标量 E2E 无回归。
- [x] T16.9 运行 M16 固定质量闸门并创建独立提交。
  验收：gofmt、vet、test、race、diff-check、markdownlint 全部通过。

## 依赖约束

```text
M13（阶段 2）→ M14 → M15 → M16
```

- M15 不引入 timeout、context、重试、并发或进程树清理。
- M16 不扩展为递归类型系统；标量 wire format 不变。
- 不新增第三方依赖；优先复用现有 source map、codec、runner 和 engine
  接口。
