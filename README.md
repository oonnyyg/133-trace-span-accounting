# trace-span-accounting

离线调用链分析工具：从 JSONL 服务调用记录重建父子关系，解释时间花在何处。
仅使用 Go 标准库，不连接任何监控平台。

## 输入格式

每行一条 JSON 记录：

```json
{"trace_id":"t1","span_id":"a","parent_id":"","service":"gw","operation":"handle","start_us":0,"end_us":100}
```

- `end_us` 不得早于 `start_us`，否则整批解析报错。
- 同一 `trace_id` 内 `span_id` 必须唯一；不同链路之间不会串接父子关系。
- 允许多个根、乱序输入。

## 耗时口径

- **包含耗时 (inclusive)**：片段自身 `end_us - start_us`。
- **独占耗时 (exclusive)**：自身时长减去**直接子片段**覆盖区间的并集长度。
  子区间先裁剪到父区间内（越界会记录 `child_out_of_range` 异常）；
  重叠的并行子调用只扣一次；孙片段不会重复扣除（由子片段的区间覆盖）。
- **链路总活动时间 (active)**：该链路全部片段区间的并集长度，
  不是片段时长之和，因此并行调用不会重复计时。

## 异常处理

- **父片段缺失**：孤立分支保留为额外的根，链路标记 `Complete=false`，
  记录 `missing_parent` 异常，不伪造父节点。
- **自引用或父子环**：该链路整体无效（`invalid_trace`），不参与汇总，
  其他链路继续分析。

## 跨批比较

`-compare` 指定第二批数据，按 service/operation 对齐：

- `common`：两批都存在，报告平均独占耗时变化与调用量变化。
- `appeared` / `disappeared`：仅一批存在，单列；不存在的一批**不**当作零耗时样本。

## 用法

```sh
go run . -input spans.jsonl -csv spans.csv -json summary.json
go run . -input old.jsonl -compare new.jsonl -json diff.json
```

- `-csv`：逐片段 CSV（`-` 表示标准输出），按 trace_id、span_id 固定排序。
- `-json`：JSON 汇总（链路树、service/operation 聚合、异常列表、跨批 diff）。
- 两个输出都省略时，JSON 汇总打印到标准输出。

## 测试

```sh
go test ./...
```

覆盖并行子调用去重、孙片段不重复扣除、越界裁剪、父片段缺失、
自引用/环隔离、活动时间并集、乱序输入与跨批比较。
