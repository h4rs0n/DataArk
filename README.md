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

## 反馈与贡献

欢迎通过 Issue 提交建议与反馈，或直接提交 PR 参与项目共建。
