# trace-span-accounting

离线调用链分析工具（仅 Go 标准库）。从 JSONL 服务调用记录重建父子关系，
解释时间花在何处，不连接任何监控平台。

## 输入格式

每行一条 JSON 记录（字段名固定，时间均为整数微秒）：

```json
{"trace_id":"t1","span_id":"root","parent_id":"","service":"api","operation":"handle","start_us":0,"end_us":100}
```

- `trace_id` / `span_id` 必填；同一链路内 `span_id` 必须唯一。
- `parent_id` 为空表示根片段；允许一条链路多个根、记录乱序到达。
- 父子关系只在同一 `trace_id` 内解析，不同链路绝不串接。
- `end_us < start_us` 属于硬错误，整个文件解析失败。

## 耗时口径

- **包含耗时（inclusive）**：片段自身 `end_us - start_us`。
- **独占耗时（exclusive）**：自身时长减去**直接子片段**覆盖区间的**并集**长度。
  - 子区间先裁剪到父区间内，越界部分另报 `child_out_of_bounds` 异常。
  - 并行/重叠的子调用只扣一次，不重复扣除。
  - 孙片段不再次扣除（它已在父的子片段处扣过）。
- **链路总活动时间（active）**：该链路全部片段区间的**并集**长度，
  而不是片段时长之和（并行调用不会被重复计入）。

## 异常与有效性

- **缺失父片段**：保留孤立分支为额外地根，链路标记 `incomplete`，不伪造父节点。
- **链路无效**（整条剔除，不影响其他链路）：`span_id` 重复、自引用、父子环。
- 所有非致命问题以 anomaly 形式列入汇总 JSON（`missing_parent`、
  `child_out_of_bounds`、`self_parent`、`cycle`、`duplicate_span_id`）。

## 命令行

```sh
# 单批分析：逐片段 CSV + JSON 汇总（省略 -summary 时打印到 stdout）
go run ./cmd/traceanal analyze -in spans.jsonl -spans spans.csv -summary summary.json

# 两批比较：按 service+operation 对齐
go run ./cmd/traceanal compare -a old.jsonl -b new.jsonl -out diff.json
```

- CSV 列：`trace_id, span_id, parent_id, service, operation, start_us, end_us,
  inclusive_us, exclusive_us, trace_incomplete`，按 `(trace_id, start_us, span_id)` 排序。
- 汇总 JSON 中 `operations` 按 `(service, operation)` 排序，含调用数、
  包含/独占耗时及均值。
- 比较输出分三组：`matched`（两批都有，报告平均独占耗时变化与调用量变化）、
  `new`（仅新批出现）、`removed`（仅旧批存在）。单边出现的操作**不**参与
  耗时差值计算，不会把“不存在”当作零耗时样本。

## 测试

```sh
go test ./...
```

覆盖并行子调用去重扣除、孙片段不重复扣除、缺失父片段孤立分支、
自引用/环/重复 ID 使链路无效、子区间越界裁剪，以及跨批比较口径。
