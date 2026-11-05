# DataArk
<div align="center">
    <img src="images/GitHub_README.png" alt="logo" width="200">
</div>

<p align="center">
<a href="README_en.md">English</a>
</p>

**DataArk - 数据方舟**是一个离线保存与数据检索系统，旨在保存互联网上可能失效的网页等数据。目前仅支持HTML网页文件存储索引，后续将支持图片视频等类型。

## 中间件
搜索引擎: [Meilisearch](https://github.com/meilisearch/meilisearch)

HTML下载: [SingleFile](https://github.com/gildas-lormeau/SingleFile)

## 安装/部署
**使用Docker Compose（推荐）**

先将 `docker/.env.example` 复制为 `docker/.env` 并填写配置。`JWT_SECRET` 留空时，每次启动自动生成一个密码学安全的随机密钥，同一进程内保持不变；重启后需重新登录。若需跨重启保持会话或运行多个实例，可使用 `openssl rand -hex 32` 单独生成并配置固定密钥（至少 32 字节），不与 `MEILI_MASTER_KEY` 共用。首次升级到独立 JWT 密钥后需重新登录。二进制也可直接读取 `JWT_SECRET`，显式 `-jwt-secret` 参数优先。非空但过短的密钥会拒绝启动。轮换搜索密钥不影响登录，轮换 JWT 密钥会使现有会话失效。
```
cd docker
sudo docker compose build
sudo docker compose up -d
```
第一次启动会生成一个初始用户名密码，请通过执行命令 `sudo docker compose logs` 查看输出中的默认用户名密码，仅在系统第一次部署运行时输出。

`dataarkapi` 会把应用、任务、数据库和 HTTP 访问/恢复日志同时写入容器控制台与宿主机 `docker/logs/`。日志按服务器本地自然日保存为 `dataarkapi-YYYY-MM-DD.log`，默认保留当天在内的最近 7 天；可在 `docker/.env` 中通过 `LOG_RETENTION_DAYS` 设置正整数天数。首次生成的管理员密码仅输出到控制台，不写入日志文件。日志目录或当日日志文件无法创建时，API 会拒绝启动，避免静默丢失日志。

**使用make编译**
```
make web
make build
```

## 文档

数据库设计、各子系统运维手册、历史事件调查与前端开发参考见 [docs/README.md](docs/README.md) 索引。

## 反馈与贡献

欢迎通过 Issue 提交建议与反馈，或直接提交 PR 参与项目共建。
