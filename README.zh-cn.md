# 鹿乃まほろ / status board

React + Vite + shadcn/ui 风格组件制作的非官方资料整理页。页面本身只读取本地 SQLite 快照，不会在浏览器里直接抓取 X 或 YouTube。

## 项目文档

- [Agent 指南](AGENTS.md)：代码边界、数据契约和协作规则。
- [文档索引](docs/README.md)：概览、架构、开发、同步和安全说明。
- [安全策略](SECURITY.md)：敏感信息和问题报告边界。

## 本地运行

```bash
npm install
cp .env.example .env
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

管理页只允许手动输入 `/admin` 访问，主页不会显示入口。
`APP_MODE=development` 时免登录；运行 `npm start` 前，应在不会提交的
`.env` 中设为 `APP_MODE=production`，并配置至少 12 位的
`ADMIN_PASSWORD`。暂时不使用自动日程识别时，`OPENAI_API_KEY` 可以留空。

## 数据与 API

SQLite 文件位于 `data/kano.sqlite`。服务端启动时会自动建表，并在空表中写入 `server/seed-data.js` 的初始快照。主要表包括 `profiles`、`posts`、`events`、`videos`、`focus`、`timeline`、`resources`、`assets`、`media_assets`、`media_links`、`sync_runs`、`sync_state`、`event_sources`、`app_settings` 和 `schedule_extractions`。

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

- X：首次最多回溯 7 天，后续只请求未知状态 ID，并按可配置的小额度刷新已知项。
- YouTube：首次保存 RSS 中最近 6 条，后续只保存游标之后的新条目，同时持续复查仍活跃的预约。
- 媒体：下载白名单内的 X 图片和 YouTube 缩略图，写入内容寻址缓存。
- 日程：命中 schedule 关键词的帖子会在配置密钥后，通过 OpenAI Responses
  API 结合帖子文字与缓存图片生成严格结构化结果。

同步失败时不会清空已有数据，会在 `sync_runs` 中记录失败原因；模型返回非法结构时也会保留旧日程。OpenAI 密钥只从进程环境读取，管理页和 SQLite 只保存模型名。可用环境变量调整来源或跳过某一来源：

```bash
X_HANDLE=kano_2525 YOUTUBE_CHANNEL_ID=UCShXNLMXCfstmWKH_q86B8w npm run sync
SKIP_X=1 npm run sync
SKIP_YOUTUBE=1 npm run sync
```

完整的首抓窗口、请求预算、媒体限制和分阶段跳过选项见
[.env.example](.env.example)。

管理页可以新增、编辑、确认和删除日程。任何人工操作都会把该记录标为
“人工确认”并永久锁定，后续平台同步或 LLM 结果不能覆盖或复活它；主页日历会区分“自动识别”和“人工确认”。

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
