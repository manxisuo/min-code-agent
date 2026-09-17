# Go Testing

本项目测试规范。在修改 Go 代码后，按此技能执行验证。

## 原则

1. 每个新增导出函数/方法尽量有单测。
2. 优先表驱动测试（table-driven），用匿名 struct 切片组织用例。
3. 测试文件与源文件同目录，命名 `xxx_test.go`。
4. 临时目录一律用 `t.TempDir()`，不要写死路径。
5. 失败信息要能看出期望值与实际值。

## 必测场景

文件类工具（read/write/edit/glob/grep）：

- 正常读写
- 路径不存在
- workspace 逃逸（`../`、绝对路径）
- 超大输入截断
- 边界：空文件、空目录

Shell 工具：

- 退出码 0 / 非 0
- 超时
- 危险命令被策略拒绝

Agent / Context：

- Fake Provider 驱动的 tool-call 闭环
- Context Snapshot 中 source / token 字段正确

## 命令

```bash
gofmt -w .
go vet ./...
go test ./...
go test ./internal/xxx/ -run TestName -v
```

提交前必须三条全绿。若某测试偶发失败，先修测试或实现，不要用 `-count=1` 掩盖。

## 代码风格

- 测试里不要引入第三方 assert 库，用标准 `testing`。
- 断言失败时打印关键变量，例如 `t.Fatalf("got = %q, want %q", got, want)`。
- 避免 `time.Sleep` 同步；需要时序时用 channel 或显式等待。
