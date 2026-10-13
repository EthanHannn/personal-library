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

GB18030 解码与旧版镜像的 `TextDecoder` 对齐（Node 20.20.2 / ICU 78.2），补齐 Go 解码库缺少的私用区和新版标点映射。测试包含 63,360 组双字节/BMP 四字节序列的独立参考哈希、补充平面字符、分块与随机输入。`tools/m0/encoding-reference.mjs` 可在旧版运行时重新生成参考；更改解码规则时须更新内容版本。

原型返回原始解码正文，尚未实现界面、标题/缩进规范化、个人数据同步或封面任务。安全边界、版本校验和编码错误具有自动化测试。

## M0 只读原型

使用 Python 3 的标准库生成 6,761 部书目、5,844,097 条章节索引、256 MiB 大 TXT 及编码/超长行样本（约需 700 MiB 磁盘）。输出目录必须不存在或为空。

```sh
python tools/m0/fixture.py --output .local/m0
go run ./cmd/library verify-legacy --db .local/m0/library.db --novels .local/m0/novels --manifest .local/m0/manifest.json
go run ./cmd/library probe --db .local/m0/library.db --novels .local/m0/novels --manifest .local/m0/manifest.json
```

启动前设置以下测试环境变量；缺少配置时服务拒绝启动：

```text
PORTAL_ALLOWED_USER=owner
LIBRARY_PUBLIC_URL=https://library.example.test
PORTAL_AUTH_URL=https://auth.example.test
```

服务默认只监听 `127.0.0.1:8080`。测试请求提供 `Remote-User: owner`；这是受控本地测试身份，生产中必须由 nginx 在 Authelia 认证后覆盖注入，后端端口只能供可信网关访问。

| 路径 | 行为 |
| --- | --- |
| `/api/health` | 容器内部探活；公网仍应经 nginx 认证 |
| `/api/v1/me` | 默认读者与统一退出地址 |
| `/api/v1/books/1/chapters?after=0&limit=100` | 按章节号定位，最多 200 条 |
| `/api/v1/books/1/chapters/1/content` | 首个正文块；用返回的 `nextCursor` 继续读取 |

所有响应为 `private, no-store`。续读游标绑定作品、章节与文件版本，并由服务签名；服务重启后需要重新打开章节。数据库使用只读连接、每连接 8 MiB 页缓存，正文读取并发上限为 4，包含响应写出的 HTTP 并发上限为 8（超限返回 429）。文件大小和修改时间检查不等同于完整指纹校验，原型要求输入快照及正文保持不变。

`verify-legacy` 是本地只读校验命令，无需门户环境变量。它运行 SQLite `quick_check`，按索引顺序扫描全部章节，检查编号、数量、孤儿、重叠、文件版本及字节边界；错误示例最多保存 100 条，总数不截断。它不会自动修复源数据，也不代替迁移阶段的完整指纹、正文解码及个人数据关联验收。

### Linux 容器测量

交叉编译 `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '-s -w' -o bin/library-linux-amd64 ./cmd/library`，再用 `docker build -f deploy/Dockerfile.probe -t personal-library:m0 .` 构建无 root 权限的原型镜像。Windows PowerShell 需以 `$env:GOOS='linux'` 等方式设置进程环境变量。

容器只映射回环端口，将 `.local/m0` 只读挂载至 `/fixture` 并传入上述测试环境变量。先以相同的 1 核 / 768 MiB 配额顺序比较旧、新服务，再单独验证新版 256 MiB 的目标上限。旧版镜像应核对为 `ec14e64`，使用同一合成数据库/正文和测试身份，并关闭封面自动抓取。旧版可直接用 `node server.js` 启动，绕开仅用于正常部署的初始化写入；合成库已经含默认读者。

```sh
node tools/m0/measure.mjs --target go --base http://127.0.0.1:16871 --container personal-library-m0 --fixture .local/m0/fixture-summary.json --out .local/reports/go.json --seconds 1800
```

旧版测量将 `--target` 改为 `legacy`，指定其回环端口和容器名，并把同一静态二进制只读挂载到 `/bench/library`，加 `--collector /bench/library`。每份报告使用新文件名。采样包含 PID 1 RSS、cgroup v2 的 current/anon/file、p50/p95、响应大小和错误；只覆盖回环 API，不代表浏览器首次可读或门户认证性能。旧目录全量响应与新版窗口响应的大小会分别记录。

后台重任务预算使用合成图片验证：`go run ./cmd/library fixture-cover --output .local/m0/cover.jpg` 创建 4096×4096 JPEG；为原型启动命令添加 `--image-workload /fixture/cover.jpg` 后，每 10 秒串行解码一次并编码三个缩略尺寸。仅用于 M0 资源测试，不抓取网络图片、不发布封面、不修改数据库，也不代表持久任务已经实现。以 256 MiB 容器上限运行 30 分钟测量，可检查持续读取与单个图片任务并行的峰值。

测量工具默认单并发；加 `--concurrency 5` 检查五个会话的余量。报告中的 `clientNode` 为负载工具版本，应用引擎版本应从对应镜像与 `verify-legacy` 的 `sqliteVersion` 核对。

## 数据与项目文档

本地测试数据与输出放入 `.local/`。真实正文、封面、数据库、个人业务数据及凭据不提交 Git。工程设计、实施验收和进度记录继续维护在 Obsidian 的 `HomeProjects/06projects/个人小说书屋`。

旧版用于核对业务行为；当前阅读内核为独立实现。性能对比需要记录数据、平台、缓存状态及运行参数，不能将原型结果当作完整应用或生产测量结果。
