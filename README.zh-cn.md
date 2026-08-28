# 鹿乃まほろ / status board

React + Vite + shadcn/ui 风格组件制作的非官方资料整理页。页面本身只读取本地 SQLite 快照，不会在浏览器里直接抓取 X 或 YouTube。

## 项目文档

- [Agent 指南](AGENTS.md)：代码边界、数据契约和协作规则。
- [文档索引](docs/README.md)：概览、架构、开发、同步和安全说明。
- [安全策略](SECURITY.md)：敏感信息和问题报告边界。

## 本地运行

```bash
npm install
npm run dev
```

开发环境会同时启动：

- Vite 前端：`http://localhost:5173`
- Express API：`http://localhost:8787`

生产构建和启动：

```bash
npm run build
npm start
```

## 数据与 API

SQLite 文件位于 `data/kano.sqlite`。服务端启动时会自动建表，并在空表中写入 `server/seed-data.js` 的初始快照。主要表包括 `profiles`、`posts`、`events`、`videos`、`focus`、`timeline`、`resources`、`assets`、`media_assets`、`media_links` 和 `sync_runs`。

看板接口：

```text
GET /api/dashboard?days=3
```

返回 profile、最近窗口内的 X 动态、全部日程、YouTube 视频与预约、最近焦点、时间轴、资料入口、schedule 图片和同步元数据。`days` 可设为 1–30。

远程图片会登记到 `media_assets`，并通过 `media_links` 关联推文、视频和其他内容。缓存完成的文件由带内容版本的 `GET /media/<opaque-id>?v=<content-sha256>` 提供；尚未缓存或缓存失败时，API 会返回 `null` 媒体地址，浏览器不会改为直连平台 CDN。运行时文件保存在已被 Git 忽略的 `data/cache/`。

健康检查：

```text
GET /api/health
```

## 更新快照

```bash
npm run sync
```

同步脚本在服务端执行：

- X：读取公开 profile 页面中的状态 ID，再请求 `api.vxtwitter.com` 的公开状态接口。
- YouTube：解析频道 RSS 获取最新视频，并检查 `/streams` 与预约视频页面中的 `scheduledStartTime`。

同步失败时不会清空已有数据，会在 `sync_runs` 中记录失败原因。同步脚本目前会登记发现的媒体，但实际下载仍是下一阶段的独立缓存任务。可用环境变量调整来源或跳过某一来源：

```bash
X_HANDLE=kano_2525 YOUTUBE_CHANNEL_ID=UCShXNLMXCfstmWKH_q86B8w npm run sync
SKIP_X=1 npm run sync
SKIP_YOUTUBE=1 npm run sync
```

X / YouTube 的公开页面可能受到限流、登录墙或页面结构变化影响，因此同步结果应以原平台页面为准。页面上的“重新读取”只重新请求 SQLite API，不会触发外部抓取。

如果希望自动保持快照，可以用系统 cron、launchd 或其他任务调度器每天（或每几小时）执行一次 `npm run sync`；看板服务本身不会在每次打开页面时抓取外部平台。

## 重新写入初始数据

```bash
npm run seed
```

该命令幂等地补充缺失的种子记录；需要覆盖已有同 ID 记录时再使用：

```bash
npm run seed -- --overwrite
```

`public/assets` 只保留固定的头像、横幅等兜底素材；会随平台内容变化的缩略图和 schedule 图片属于 `data/cache/` 运行时缓存，不进入 Git。页面为 fan-made 项目，相关平台链接均指向原始页面。

## 本地检查

```bash
make check-sensitive
make check-docs
make ci
```

`make ci` 会安装锁定依赖、运行敏感信息、服务端缓存契约和文档检查，并验证生产构建；它不会执行实时 X/YouTube 同步。
