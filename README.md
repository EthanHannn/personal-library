# personal-library

个人小说书屋。采用 Go + SQLite + Vue 3 / Vite 的独立重写项目，优先改善阅读流畅度、打开速度与服务器占用。

M0–M2 阅读主链及 M3 首页/书架、检索、本人投票、本地推荐、个人中心、书库管理、TXT 导入和真实封面任务已实现。起点采集支持公开免费章节的预览入库与断点恢复；实际访问仍可能被站点验证阻止。现有线上书屋仍由 [zhixuan-library](https://github.com/EthanHannn/zhixuan-library) 提供服务，真实迁移与生产替换尚未完成。评论/回复、失效原站入口和个人数据导出不在当前范围内。

## 开发

需要 Go 1.27.1 和 Node.js 24（仅构建前端时使用）。依赖版本由 `go.mod` / `go.sum` 及 `web/package-lock.json` 锁定。

```sh
go test ./...
go vet ./...
go test ./internal/reader -bench . -benchmem
npm --prefix web ci --ignore-scripts
npm --prefix web test
npm --prefix web run build
go test -tags release ./...
```

阅读内核通过 `os.Root` 将文件访问限制在书库内，用 `ReadAt` 读取有上限的字节范围。支持 UTF-8、UTF-8 BOM 和 GB18030，正文块最多 256 KiB UTF-8；续读位置保留原始文件字节偏移。编码由调用方提供，读取时不猜测或转写原文件。

GB18030 解码与旧版镜像的 `TextDecoder` 对齐（Node 20.20.2 / ICU 78.2），补齐 Go 解码库缺少的私用区和新版标点映射。测试包含 63,360 组双字节/BMP 四字节序列的独立参考哈希、补充平面字符、分块与随机输入。`tools/m0/encoding-reference.mjs` 可在旧版运行时重新生成参考；更改解码规则时须更新内容版本。

正文 API 保留原始解码文本，界面将重复的首行章名合并到标题展示。阅读器支持目录、按号跳章、邻章预取、六套主题、字号、彩色正文和浏览器 TTS，以及本机暂存、服务端保存与冲突选择。

## 正式服务与静态应用

完成下方迁移后，设置门户测试环境变量并运行：

```sh
go run ./cmd/library serve --db .local/m1/library.db --novels .local/m0/novels --web-dir web/dist
```

默认监听 `127.0.0.1:8080`。`serve` 从新业务库查询文件版本，无需再传外部 manifest；未完成校验的迁移库、缺失或不匹配的门户读者映射会阻止启动。正常启动只检查 schema 和身份映射，不全表扫描。

当前 schema 为 v4，升级前先备份业务库并显式执行 `library migrate --db <数据库>`。`/profile` 汇总个人记录和本机阅读设置；`/manage` 提供书目审核/修正、TXT 预览与确认、目录检查、多书源搜索与公开章节采集、单本及批量封面补全。导入只新增作品或补充缺失正文，不覆盖已有正文；来源评分与个人关系保留。规则推荐只使用已有作者/分类等元数据，候选不足时不展示。

`--covers` 和 `--collected` 默认分别为数据库同级的 `covers/`、`collected/`，随可写数据目录持久化。旧封面复制到新封面根目录，保留 `real/` 等子目录。采集内容通过保留路径前缀 `_collected/` 读取，原小说根目录不能占用该名称。单 worker 顺序处理持久任务，可取消、重试及恢复；默认网络请求间隔为 5 秒，自定义书源可配置 1–60 秒，验证页和 403/429 会触发 6 小时来源冷却。起点付费章不采集，部分正文会明确标记；浏览器采集工具生成的 TXT 可经同一导入流程处理。取消任务保留暂存文件，暂不自动清理。

Vue 3 + TypeScript + Vite + Vue Router 静态构建支持 `/` 首页、`/library`、`/category/:tag`、`/author/:name`、`/bookshelf`、`/book/:id`、`/read/:id?c=N`。分类/作者名称按一个 URL 段编码，可包含中文及特殊字符。首页包含精选轮播、最近阅读、高分和最近入库；精选从最多 30 部高分且有正文的仙草票候选中随机取 5 部，轮播由读者手动切换。馆藏统计覆盖全部已审核书目，高分栏目只是一种浏览预设，低分或无正文书目仍可收藏。

书库默认显示 7.5 分以上且有正文的作品，可切换到全部馆藏。搜索、主副分类、评分/字数范围、正文筛选、排序与页码保存在 URL 中，刷新与后退会恢复；分类和作者页沿用同一套筛选。搜索仅匹配书名和作者，支持中文单字与双字，`%`、`_` 和引号均按字面处理；ASCII 不区分大小写，不做正文检索或别名扩展。分类目录的计数覆盖全部已审核馆藏，结果区统计则对应当前筛选。

书架收藏与阅读记录独立：收藏不创建进度，移出不删除进度。`/bookshelf?view=reading` 显示所有近期阅读，不要求收藏。两类列表每页默认 24 条（最多 48），以时间和原记录 ID 组成游标，重复收藏保留原顺序；列表持续变化时可刷新返回最新记录，并非固定快照。首页/书架的续读链接不强加章号，由阅读器协调服务端与本机待同步位置。

阅读页无章号时恢复进度，显式章号优先。目录每窗 100 条并虚拟渲染；正文逐块追加，附近最多 3 章且总文本预算约 2 MiB，超长章节分窗继续，有文本选择时不替换旧窗口。预取在后台页面及节省流量模式暂停。没有额外登录表单。

进度约每秒写入 IndexedDB，约每 7 秒合并同步，并在切章/隐藏/离开时补发。已发送请求与更新的本机草稿分开保存，重复发送复用原 mutation；409 由读者选择本机或服务端位置。多标签用 BroadcastChannel 与 IndexedDB 事务协调，最终由服务端版本检查保护。401/403 会停止朗读和预取、清空当前正文，保留待同步位置；重新进入前台先验证会话。

同源旧 `zx_reading_*` 与阅读偏好键首次打开时复制到新命名空间，旧键保留。旧版 percent 按章节内容比例近似恢复，成功阅读后才保存精确锚点；其他域名的浏览器状态不自动迁移。页关闭前发送仅为尽力处理，可靠恢复依靠已经完成的本机暂存。浏览器存储被禁用时明确显示无法保存。

TTS 使用设备的 Web Speech 音色，支持语速、暂停/恢复、前后段、高亮跟随及跨正文块续读。隐藏页面时暂停，切章、失去认证和卸载时停止。设备音色/音频和手机锁屏行为仍需在目标设备上验收。

| 路径 | 行为 |
| --- | --- |
| `/api/v1/me` | 数据库中的默认读者及统一退出地址 |
| `/api/v1/home` | 有上限的精选/高分/最近入库、3 条最近阅读与馆藏统计 |
| `/api/v1/shelf?cursor=...&limit=24` | 当前读者的收藏与相关进度；按收藏时间和 ID 分页 |
| `/api/v1/reading?cursor=...&limit=24` | 当前读者的阅读记录，独立于收藏；按最近阅读时间和 ID 分页 |
| `/api/v1/books/:id/shelf` | PUT 加入、DELETE 移出；JSON 请求体 `{}`，可安全重试 |
| `/api/v1/books?page=1&limit=24` | 参数化搜索/筛选、六种排序、页码及总数；每页最多 48 本 |
| `/api/v1/categories` | 全部已审核馆藏的主分类、副分类及计数 |
| `/api/v1/books?after=0&limit=24` | 保留 M1 的 ID 递增窗口；不能与筛选或页码参数混用 |
| `/api/v1/books/:id` | 已审核书目的基本详情、收藏状态及进度 |
| `/api/v1/books/:id/chapters?after=0&limit=100` | 当前内容版本的目录窗口 |
| `/api/v1/books/:id/chapters/:idx/content` | 有界正文块及签名续读游标 |
| `/api/v1/books/:id/reader?chapter=N` | 聚合首次正文、服务端进度、书架状态与邻章；显式章号优先 |
| `/api/v1/books/:id/directory?after=0&q=标题` | 作品内按号分页及标题检索，每次最多 100 条 |
| `/api/v1/books/:id/progress` | GET 当前进度；PUT 比较版本并原子保存 |

书目分页 API 接受 `search`（最多 100 字）、`author`（精确作者）、`tag`（主/副分类任一匹配）、`tag1`、`tag2`、`minScore/maxScore`（0–10）、`minWords/maxWords`、`onlyContent`（0/1），以及 `sortBy=score|xiancao|popularity|latest|words|title`。按评分/仙草票/评价人数/入库时间/字数降序或书名升序，再以 ID 降序打破平局。API 未指定筛选时包含全部已审核书目；浏览器显式传入自己的预设。无 `page` 或任何筛选参数时，仍返回 M1 窗口协议。

页码范围 1–10,000，超出实际末页时返回末页，空结果返回第 1 页和 `totalPages=0`。同一次请求的计数与书目共用读事务快照；多次翻页之间如有评分或元数据更新，顺序可以变化。未知、重复参数、非法数值和倒置范围返回 400；客户端不能通过 `status` 读取待审核书目。查询只访问书目及评分快照，未增加 FTS、章节扫描、缓存或新 schema 迁移。

进度 PUT 包含 `chapterIdx`、`percent`（0–1）、`contentVersion`、`anchor: {block, paragraph, character}`，以及 `baseRevision`、`deviceId`、`clientSeq`、`mutationId`。`block` 使用正文响应的 `blockToken`；段落起点和段内位置按 Unicode 码点计数。服务端验证签名、文件版本和段落范围，冲突返回 409 及当前/提交位置。每本书保留最近 128 次成功请求的原始回执，持久化设备序号防止更旧的重试覆盖新值。签名密钥保存在数据库中，随备份保留，正式服务重启不使锚点失效。

HTML、脚本、API 均处于同一认证边界，使用 `private, no-store`；缺失 API/资源返回 JSON 404，SPA 回退只接受明确的页面路由。`/login`、`/register`、`/auth/signin` 固定跳回本站首页，由既有网关处理登录。`/api/health` 只在应用内部免身份，公网 nginx 路由仍须认证。浏览器端不会自行添加 `Remote-User`。

正常查询由固定版本 sqlc 生成，修改 SQL 后运行：

```sh
go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate
```

### 单容器发布骨架（本地演练）

前端构建后，交叉编译 `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags release -trimpath -ldflags '-s -w' -o bin/library-linux-amd64 ./cmd/library`，再执行 `docker build -f deploy/Dockerfile -t personal-library:local .`。PowerShell 需通过 `$env:GOOS='linux'` 等设置变量。`release` 构建将 `web/dist` 嵌入 Go 二进制，运行时没有 Node 服务。

`deploy/compose.yml` 用于本地演练，默认映射回环端口 16871，限制 1 CPU / 256 MiB。设置 `LIBRARY_DATA_DIR`（含已完成迁移的 `library.db`）、`LIBRARY_NOVEL_ROOT`（只读正文）及三个门户环境变量，再执行 `docker compose -p personal-library-local -f deploy/compose.yml up -d`。数据目录必须已存在，并对容器 UID/GID 65532 可写；正文目录只需可读。使用独立演练目录，当前不能据此替换生产旧站。

CI 在 Windows/Linux 上运行前端测试、构建和 Go 测试；Linux 另检查 race、sqlc 生成一致性、合成导入及真实发布容器的基础、进度、首页/书架、检索和重启恢复。可复用 `node tools/m1/smoke.mjs --base http://127.0.0.1:16871`、`node tools/m2/smoke.mjs --base http://127.0.0.1:16871 --out .local/progress-smoke.json` 和 `node tools/m3/smoke.mjs --base http://127.0.0.1:16871 --out .local/shelf-smoke.json`。M2/M3 脚本在重启后分别加 `--resume` 和对应报告路径，验证持久进度/回执与收藏。书库只读回归用 `node tools/m3/catalog-smoke.mjs --base http://127.0.0.1:16871`。这些脚本只用于独立合成库。

本地浏览器测试可启动 `node tools/m2/browser-gateway.mjs`，访问 `http://127.0.0.1:16872`。该回环测试网关使用合成会话和可控 503/401，只用于故障回归，不是 Authelia，也不用于生产部署。`tools/m2/measure-progress.mjs` 可与 M0 读取测量脚本同时运行，记录同步写入延迟。

M3 首批可用 `node tools/m3/measure.mjs --base http://127.0.0.1:16871 --container personal-library-m3 --fixture .local/m0/fixture-summary.json --seconds 30 --out .local/reports/m3-mixed-read.json` 做 5 路混合读取短测。每轮各请求一次首页、书架、最近阅读、详情和正文；报告记录延迟、响应字节及容器内存。它不包含封面/导入任务、写入或门户开销，不代替 M4 完整性能验收。

检索演练可先用 `python tools/m3/catalog-fixture.py --source .local/m3/library.db --output .local/m3-catalog` 创建独立副本；目标目录必须不存在。脚本使用 SQLite backup，变化书目元数据以覆盖分类、评分和特殊字符，保留正文、章节索引及前 5 本基础样本。将容器指向该副本后，给上述测量命令加 `--catalog`，并把 `--fixture` 改为 `.local/m3-catalog/fixture-report.json`，测量深页短词搜索、组合筛选、作者、分类和分类计数。`python tools/m3/catalog-plans.py --db .local/m3-catalog/library.db --out .local/reports/catalog-plans.json` 记录 Python SQLite 的查询计划；发布性能由 Go 容器请求单独实测。

## 业务库与旧库迁移

先通过 SQLite backup API/CLI 取得**离线一致性快照**；不能只复制运行中的 WAL `.db`。导入命令拒绝带非空 WAL/journal 的输入，原库及 TXT 均保持只读。目标数据库放在本地磁盘，使用独立新路径，不放在 OneDrive 或网络文件系统。

```sh
go run ./cmd/library migrate --db .local/m1/library.db
go run ./cmd/library import-legacy --source .local/m0/library.db --db .local/m1/library.db --novels .local/m0/novels --manifest .local/m0/manifest.json --portal-user owner --source-version synthetic-m0-96abf8d
```

目标目录须先存在。`migrate` 显式创建或升级 schema，当前为 v4；升级保留既有阅读记录、采集任务与迁移校验值。升级前应备份业务库和 collected/covers 目录，使用新版本显式运行 migrate 后再启动服务。它拒绝旧版 Prisma 数据库，正常服务启动不会迁移或扫描全库。业务库使用 WAL、一个写连接/两个读连接、每连接 foreign_keys/busy_timeout/cache 设置，写入 synchronous=FULL。

导入默认每 5,000 行提交一次；`--batch-size` 最大 10,000，`--batch-pause 10ms` 可限速，`--max-batches N` 可主动停在检查点。相同命令继续执行会校验输入指纹后续传；源快照、正文、编码清单、门户用户或来源版本变化时拒绝继续。导入必须离线独占目标库。源数据异常不会静默删行或修正，修正输入后使用另一份新目标库重新演练。

书目、章节、历史读者、书架、进度、投票和评论保留原 ID；默认读者映射到指定门户用户，其他读者保留历史归属，不创建密码或登录入口。章节标题/偏移保持原值，原路径另行保留；同文件允许关联多本书。时间统一为 UTC 毫秒，支持 Prisma 毫秒整数、UTC SQL 时间和带时区 ISO 时间。未识别时间直接失败。

历史评分与票数存入 `rating_snapshots`，已导入个人票同时保存 `imported_type` 基准，后续改票须按差量更新，不能再次累加全部个人票。回复分两步迁移，并检查缺失父评论、跨作品回复和循环关系。

标准输出为 JSON 报告：11 类实体的行数/规范化逐字段 SHA-256、全部源文件 SHA-256、源库全章节校验和目标库 `integrity_check`。检查失败或中断的目标不会被服务接受。清单只提供已确认的编码；此阶段不自动识别编码、不迁移浏览器尚未同步的进度、不复制或验证封面图片。真实全量解码、封面与浏览器状态验收继续按 M4 清单执行。

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

## 网络采集与添加书源

进入「书库管理 → 我的书源 → 添加书源」，可从网页 CSS、JSON 接口或站点模板开始，也可在说明区域粘贴本项目格式的完整 JSON。填写站点域名、编码、请求间隔和 search/book/toc/chapter 规则后保存，再用样本作品链接检查详情、目录和首章。支持编辑、启停和版本冲突检测；仅启停的起点内置适配器继续保留。

「网络采集」提供作品链接与多源搜书两种入口。搜书每次最多 8 个源、每源前 20 个候选，按书名和作者比较来源；选择后生成采集任务，最终仍需确认预览才会入库。任务保存创建时的规则、目录指纹和逐章检查点，重试不会改用编辑后的规则。遇到目录变化需新建任务。旧版已有的可读正文、收藏、投票和阅读进度不会因采集被覆盖；自动追更、换源替换和进度映射尚未提供。

规则为本项目格式，HTML 使用 CSS 与属性提取，JSON 使用 RFC 6901 Pointer；不直接兼容 Legado/阅读书源、XPath、JS 或需浏览器渲染的站点。搜索支持 GET 或表单 POST；目录最多 50 页、单章最多 20 页，逐章保存，完整性只相对该源当前目录。配置中的请求只访问列出的公共域名，DNS 和每次重定向也会检查；不使用系统代理环境变量。

站点模板是需要检查的起点：2026-10-14 小样本中，香书的搜索/目录/首章通过，书海阁的目录及两页首章通过，但其后台搜索返回站点错误，模板仅启用链接采集。两站模板由当前页面结构独立编写，不引入上游采集器运行时；规则职责参考 [so-novel](https://github.com/freeok/so-novel) 等，完整选型和版本证据记在 Obsidian。当前正文处理不等于对所有作品或全目录的质量保证。

新增回归检查：`node tools/m3/sources-smoke.mjs --base http://127.0.0.1:16871`；必须使用独立合成库。网络观察测试默认跳过，可通过环境变量 `LIBRARY_SOURCE_LIVE=1` 运行 `go test ./internal/management -run TestLiveSourceSamples -v`，仅检查样本元数据、目录和首章，不入库。

已审阅并改写成本项目格式的书源包，可批量加载到本地独立库并验证：

```powershell
node tools/m3/load-source-pack.mjs --pack .local/reviewed-sources.json --out .local/source-check-report.json
node --test tools/m3/load-source-pack.test.mjs
```

工具默认访问 `http://127.0.0.1:16871`，使用合成读者 `owner` 和测试 Origin；只允许回环 HTTP 服务。包的 `format` 为 `personal-library/source-pack@1`，`sources` 为数组；每项含完整 `config`、`sample`（`url/title/author`，支持搜索时还需 `query`）及 `provenance`（`repository/revision/path`，建议补充 `license/notes`）。上游原文件需先审阅和适配，工具不会执行脚本或自动转换阅读书源。

工具复用相同配置，遇到同名不同规则时报告冲突；逐源检查详情、目录、首章及可选搜索，并比对预期书名/作者。检查失败会停用该配置，报告保留任务、规则版本、错误和最终启停状态；返回码 1 表示有未通过项，须查看报告。搜索检查会确认有候选，目标作品是否包含其中仍应在多源搜索结果中核对。检查不生成入库任务，也不承诺全本完整。原始书源、配置包和在线报告保存在 `.local/`，具体仓库版本、可用源和环境差异记入 Obsidian。

## 数据与项目文档

本地测试数据与输出放入 `.local/`。真实正文、封面、数据库、个人业务数据及凭据不提交 Git。工程设计、实施验收和进度记录继续维护在 Obsidian 的 `HomeProjects/06projects/个人小说书屋`。

旧版用于核对业务行为；当前阅读内核为独立实现。性能对比需要记录数据、平台、缓存状态及运行参数，不能将原型结果当作完整应用或生产测量结果。
