<div align="center">
  <img src="public/assets/kano-avatar.jpg" alt="鹿乃まほろ" width="96">
  <h1>Kano Navi</h1>
  <p><strong>把鹿乃まほろ的近况、日程与旅程，放在同一个地方。</strong></p>
  <p>
    <a href="https://github.com/yexca/kano-navi/actions/workflows/ci.yml"><img src="https://github.com/yexca/kano-navi/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
    <a href="https://github.com/yexca/kano-navi/actions/workflows/release.yml"><img src="https://github.com/yexca/kano-navi/actions/workflows/release.yml/badge.svg" alt="Release"></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0-blue" alt="许可证：AGPL v3"></a>
  </p>
  <p>
    <a href="https://kano.yexca.net/"><strong>在线访问</strong></a> ·
    <a href="#功能概览">功能概览</a> ·
    <a href="#使用-docker-快速部署">快速部署</a> ·
    <a href="docs/README.md">项目文档</a> ·
    <a href="https://github.com/yexca/kano-navi/releases">版本发布</a>
  </p>
  <p><a href="README.md">English</a> · 简体中文</p>
</div>

Kano Navi 是为 **鹿乃まほろ（Kano Mahoro）** 的粉丝制作的非官方、可自行部署的近况看板。
查看下一场配信，浏览 X 与 YouTube 的最近更新，也可以沿着历程与图片图鉴回顾她从 2010 年至今的公开活动。
界面支持日语、英语、简体中文和明暗主题，适配桌面与手机。

## 界面预览

**[在线访问 Kano Navi](https://kano.yexca.net/)**，或前往
[旅程与图片图鉴](https://kano.yexca.net/history)回顾公开活动与历史影像。
截图来自首次启动的本地实例，来源内容将在运行工作流后填充。

![Kano Navi 近况看板，浅色主题](docs/assets/readme/dashboard.jpg)

<details>
<summary>展开旅程页面预览</summary>

![Kano Navi 旅程页面，浅色主题](docs/assets/readme/history.jpg)

</details>

## 功能概览

| 模块           | 可以做什么                                                                       |
| -------------- | -------------------------------------------------------------------------------- |
| 近况看板       | 一眼查看下一场配信或活动、日本时间倒计时与最近动态。                             |
| 每周日程       | 按周浏览活动列表与日程图片，区分自动识别和人工确认。                             |
| X 与 YouTube   | 聚合两个 X 账号的动态、视频更新与预约配信，展示精选视频。                        |
| 旅程与图片图鉴 | 按活动身份和年份浏览里程碑，查看附有来源与署名的本地图片档案。                   |
| 管理后台       | 手动或定时运行更新工作流、整理日程、管理头像与横幅，以及配置可选的 AI 日程识别。 |
| MCP 集成       | 读取公开快照，通过独立鉴权的控制工具启动自动任务。                               |

## 使用 Docker 快速部署

需要 **支持 Linux 容器的 Docker 与 Docker Compose**。

1. 将 [docker-compose.yml](docker-compose.yml) 和 [.env.example](.env.example)
   保存到同一目录，将 `.env.example` 复制为 `.env`。
2. 编辑 `.env`：保持 `APP_MODE=production`，配置去掉首尾空白后
   **至少 12 个字符**的 `ADMIN_PASSWORD`。
3. 启动应用：

   ```bash
   docker compose up -d
   ```

打开 **[localhost:7657](http://localhost:7657/)**。
管理后台位于 `/admin`，需要手动输入路径，并使用刚才配置的密码登录。

Compose 默认拉取 `yexca/kano-navi:latest`。数据库、缓存媒体和后台配置保存在
`./data` 中，升级时会保留。再次运行 `docker compose up -d` 即可拉取更新并替换容器。

固定版本、GHCR 镜像、自定义端口和反向代理配置见
[Docker 部署说明](docs/operations/docker.md)。

### 首次使用

个人资料、历程、资源链接和历史图片开箱即用。
X 动态、YouTube 视频、日程与 AI 服务配置初始为空。

- 在 `/admin` 中运行包含 X、YouTube 和媒体缓存步骤的工作流，填充看板内容。
- 如需定期更新，为保存的工作流启用定时器。
- 如需 AI 日程识别，在 `.env` 中配置 `LLM_SECRETS_KEY`，再在后台添加服务商、
  模型与识别路由。设置方法见[配置说明](docs/operations/configuration.md)。

## 内容如何更新

工作流在服务端收集公开内容、缓存媒体，并写入本地 SQLite 快照。
打开页面或点击看板的重新读取按钮，只会读取这份快照，不会启动平台抓取或模型请求。
来源暂时不可用时，已有快照仍然保留。

人工编辑和删除的日程受到保护，自动同步不能覆盖或复活它们。
内容新鲜度取决于来源的可用性，信息以原平台为准。
抓取覆盖范围与限制见[来源说明](docs/architecture/sources.md)和
[工作流说明](docs/architecture/workflows.md)。

## 本地开发

需要 **Node.js 24 LTS（24.19 或更新版本）**、**npm 10+** 和 Git。

```bash
git clone https://github.com/yexca/kano-navi.git
cd kano-navi
npm ci
cp .env.example .env
```

本地开发时，在 `.env` 中设置 `APP_MODE=development`，启用免密码管理后台；
设置 `WORKFLOW_SCHEDULER_ENABLED=0`，关闭定时工作流。然后启动：

```bash
npm run dev
```

前端与 API 共用 [localhost:7657](http://localhost:7657/)。
PowerShell 中的复制命令为 `Copy-Item .env.example .env`。
构建、预览和 Docker 开发环境见[本地开发指南](docs/development/local-dev.md)。

技术栈：**React · TypeScript · Vite · Tailwind CSS · Express · SQLite**。
项目通过 GNU Make 统一运行检查：

```bash
make check             # 使用已安装依赖，运行检查、构建和隔离的 API 冒烟验证
make ci                # 安装锁定依赖，运行全部检查及 Docker 运行验证
make sensitive-check   # 扫描敏感信息
```

这些检查不会执行实时来源同步。分项检查与前置条件见
[测试与 CI](docs/development/testing.md)。

## 文档导航

| 想了解……                  | 从这里开始                                                      |
| ------------------------- | --------------------------------------------------------------- |
| 项目定位与页面功能        | [项目概览](docs/overview.md)                                    |
| 部署、配置与备份          | [运维文档](docs/operations/index.md)                            |
| API、数据模型、来源与媒体 | [架构文档](docs/architecture/index.md)                          |
| 历史图片的来源与署名      | [历程与图片图鉴](docs/product/history.md)                       |
| 开发、检查与贡献约定      | [开发文档](docs/development/index.md) · [Agent 指南](AGENTS.md) |
| 安全边界与问题报告        | [安全策略](SECURITY.md) · [安全文档](docs/security/index.md)    |

完整指南见[文档索引](docs/README.md)。

## 致谢与许可

由 [yexca](https://github.com/yexca) 制作。应用的 `/about` 页面列有开发致谢和技术栈。

项目代码采用 [GNU AGPL v3](LICENSE) 许可证。插画、艺人名称及其他第三方内容的权利
归原权利人所有，代码许可证不授予这些素材的使用权。
图片来源与已记录的署名见[历史图鉴说明](docs/product/history.md)。

Kano Navi 是独立的粉丝项目，与官方无隶属关系，也未获官方背书。
