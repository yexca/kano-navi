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
`ADMIN_PASSWORD`。provider 密钥由管理页配置并加密保存到 SQLite；环境中只需
配置 `LLM_SECRETS_KEY`。
管理页采用经典侧栏后台布局，分为概览、分页日程、关键词/看板扫描、单条消息
扫描、多个 OpenAI-compatible 模型提供商，以及头像与横幅标签。

`/mcp` 是独立的无状态集成入口。公开读取工具不需要密钥；推进 revision、
启动同步和运行自动扫描才需要 `Authorization: Bearer <MCP_CONTROL_TOKEN>`。
该 token 与管理员密码分离，也不能执行人工确认、编辑、删除或素材选择。

## 数据与 API

SQLite 文件位于 `data/database/kano.sqlite`。服务端启动时会自动建表，并在空表中写入 `server/seed-data.js` 的初始快照。主要表包括 `profiles`、`posts`、`events`、`videos`、`focus`、`timeline`、`resources`、`assets`、`media_assets`、`media_links`、`sync_runs`、`sync_state`、`event_sources`、`app_settings`、`schedule_extractions`、`llm_providers` 和 `llm_route_providers`。

看板接口：

```text
GET /api/dashboard?days=3
```

返回 profile、最近窗口内聚合的两个 X 账号动态、全部日程、YouTube 视频与预约、手动选择的 Featured 视频、时间轴、资料入口、schedule 图片和同步元数据。`days` 可设为 1–30。

远程图片会登记到 `media_assets`，并通过 `media_links` 关联推文、视频和其他内容。缓存完成的文件由带内容版本的 `GET /media/<opaque-id>?v=<content-sha256>` 提供；尚未缓存或缓存失败时，API 会返回 `null` 媒体地址，浏览器不会改为直连平台 CDN。运行时文件统一保存在已被 Git 忽略的 `data/`：数据库在 `data/database/`，X 图片在 `data/x/`，YouTube 图片在 `data/youtube/`，管理员选中的头像和横幅在 `data/avatar/`。

健康检查：

```text
GET /api/health
```

## 更新快照

```bash
npm run sync
```

同步脚本在服务端执行：

- X：每个账号首次最多回溯 7 天，后续只请求未知状态 ID，并按可配置的小额度刷新已知项；两个账号的结果按时间聚合。
- YouTube：首次保存 RSS 中最近 6 条，后续只保存游标之后的新条目，同时持续复查仍活跃的预约。
- 媒体：下载白名单内的 X 图片和 YouTube 缩略图，写入内容寻址缓存。
- 日程看板：先按管理页单独配置的关键词阶段筛选候选，再把帖子文字与缓存图片
  交给独立的 board provider 队列；
- 单条消息：用轻量的日期/告知启发式筛选疑似日程消息，再交给独立的 message
  provider 队列；
- provider 会按照原始消息模态筛选：纯文字需要 `Text`，纯图片需要 `Image`，
  文字加图片需要同时具备两种能力。单个 provider 最多尝试三次，失败后按优先级
  切换到下一个兼容 provider。支持 OpenAI Responses 和 Chat Completions。

看板和单条消息检测共享一次扫描的总数量限制，并共同更新自动日程快照。模型返回
`uncertain` 时会缓存识别结果供检查，但不会创建日程。`SCHEDULE_MESSAGE_ENABLED`
独立控制单条消息阶段；原有的关键词和看板开关仍然分别生效。

同步失败时不会清空已有数据，会在 `sync_runs` 中记录失败原因；模型返回非法结构时也会保留旧日程。provider 密钥在管理页输入后会使用环境变量中的 `LLM_SECRETS_KEY` 加密保存到 SQLite，运行时不再读取 `OPENAI_API_KEY`。旧版密钥可通过一次性命令 `npm run migrate:llm` 导入，验证后应从环境中移除。API 不会回显明文。可用环境变量调整来源或跳过某一来源：

```bash
X_HANDLES=kano_2525,_Kanotic YOUTUBE_CHANNEL_ID=UCShXNLMXCfstmWKH_q86B8w npm run sync
SKIP_X=1 npm run sync
SKIP_YOUTUBE=1 npm run sync
```

完整的首抓窗口、请求预算、媒体限制和分阶段跳过选项见
[.env.example](.env.example)。

管理页可以新增、编辑、确认和删除日程。任何人工操作都会把该记录标为
“人工确认”并永久锁定，后续平台同步或 LLM 结果不能覆盖或复活它；主页日历会区分“自动识别”和“人工确认”。
MCP 只允许读取和启动自动任务，不会执行这些人工操作；任务完成后会递增
dashboard revision，已打开的主页会重新读取 SQLite 快照。

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

`public/assets` 只保留固定的兜底素材；会随平台内容变化的缩略图和 schedule 图片属于 `data/x/` 或 `data/youtube/` 运行时缓存，头像和横幅候选按来源写入对应目录，本地上传和人工选择后的头像、横幅属于 `data/avatar/`，均不进入 Git。页面为 fan-made 项目，相关平台链接均指向原始页面。

## 本地检查

```bash
make check-sensitive
make check-docs
make ci
```

`make ci` 会安装锁定依赖、运行敏感信息、服务端缓存契约和文档检查，并验证生产构建；它不会执行实时 X/YouTube 同步。
