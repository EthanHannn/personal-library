# personal-library

个人小说书屋。采用 Go + SQLite + Vue 3 / Vite 的独立重写项目，优先改善阅读流畅度、打开速度与服务器占用。

当前实施 M0：先验证范围读取、中文编码和全规模章节查询。现有线上书屋仍由 [zhixuan-library](https://github.com/EthanHannn/zhixuan-library) 提供服务；本项目尚不能替代生产站点。

## 开发

需要 Go 1.27.1。依赖版本由 `go.mod` / `go.sum` 锁定。

```sh
go test ./...
go vet ./...
go test ./internal/reader -bench . -benchmem
```

阅读内核通过 `os.Root` 将文件访问限制在书库内，用 `ReadAt` 读取有上限的字节范围。支持 UTF-8、UTF-8 BOM 和 GB18030，正文块最多 256 KiB UTF-8；续读位置保留原始文件字节偏移。编码由调用方提供，读取时不猜测或转写原文件。

原型返回原始解码正文，尚未实现界面、标题/缩进规范化、个人数据同步或封面任务。安全边界、版本校验和编码错误具有自动化测试。

## 数据与项目文档

本地测试数据与输出放入 `.local/`。真实正文、封面、数据库、个人业务数据及凭据不提交 Git。工程设计、实施验收和进度记录继续维护在 Obsidian 的 `HomeProjects/06projects/个人小说书屋`。

旧版用于核对业务行为；当前阅读内核为独立实现。性能对比需要记录数据、平台、缓存状态及运行参数，不能将原型结果当作完整应用或生产测量结果。
