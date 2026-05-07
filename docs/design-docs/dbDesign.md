# 数据库设计

本文档根据 `api/common/db.go` 中的 GORM 模型和数据库操作整理，用于后续开发时参考。当前后端使用 PostgreSQL，连接参数来自运行时配置，并在 `InitDB()` 中通过 `AutoMigrate(&User{}, &ArchiveTask{}, &ArchiveStat{})` 自动迁移表结构。

## 总体约定

- ORM：GORM。
- 数据库：PostgreSQL。
- 表名：使用 GORM 默认命名规则，`User` 对应 `users`，`ArchiveTask` 对应 `archive_tasks`，`ArchiveStat` 对应 `archive_stats`。
- 时区：连接 DSN 设置为 `TimeZone=Asia/Shanghai`。
- 当前没有显式外键关系，用户表、归档任务表和统计表彼此独立。

## users

用户账号表，用于登录认证和用户管理。

| 字段 | Go 类型 | 约束/索引 | 说明 |
| --- | --- | --- | --- |
| `id` | `uint` | 主键 | 用户 ID，由 GORM 管理主键生成 |
| `username` | `string` | 唯一索引，非空 | 登录用户名 |
| `password` | `string` | 非空 | bcrypt 哈希后的密码；接口 JSON 序列化时不返回 |
| `created_at` | `time.Time` | GORM 自动维护 | 创建时间 |
| `updated_at` | `time.Time` | GORM 自动维护 | 更新时间 |

### 主要操作

- `CreateUser(username, password)`：先按 `username` 查询是否存在，再使用 bcrypt 加密密码并创建用户。
- `LoginUser(username, password)`：按 `username` 查询用户，然后用 bcrypt 校验密码。
- `GetUserByID(id)`：按主键查询用户。
- `GetUserByUsername(username)`：按唯一用户名查询用户。
- `UpdateUser(id, updates)`：先按主键读取用户；如果更新字段包含 `password`，会先重新哈希再写入。
- `DeleteUser(id)`：按主键删除用户。
- `GetAllUsers(page, pageSize)`：先统计总数，再用 offset/limit 分页查询。

### 初始化行为

`InitDB()` 会调用 `createDefaultAdmin()`。如果 `admin` 用户不存在，会随机生成 12 位十六进制密码，创建默认管理员并在服务启动日志中输出账号密码；如果已存在，则跳过创建。

## archive_tasks

HTML 离线归档任务表，用于持久化 URL 归档任务状态。任务状态落库的目的，是让异步归档任务在服务重启后仍能恢复或查询。

| 字段 | Go 类型 | 约束/索引 | 说明 |
| --- | --- | --- | --- |
| `id` | `string` | 主键，长度 36 | 任务 ID，通常用于保存 UUID |
| `url` | `string` | 普通索引，非空 | 原始或标准化后的归档 URL |
| `domain` | `string` | 非空 | URL 所属域名，用于归档文件目录和展示 |
| `status` | `string` | 普通索引，非空 | 任务状态；查询逻辑将 `pending`、`running` 视为活跃任务 |
| `file_name` | `string` | 无显式约束 | 归档完成后的 HTML 文件名 |
| `error` | `string` | `text` | 失败原因或错误详情 |
| `external_task_id` | `string` | 无显式约束 | 外部 SingleFile 服务任务 ID |
| `created_at` | `time.Time` | GORM 自动维护 | 创建时间 |
| `updated_at` | `time.Time` | GORM 自动维护 | 更新时间 |
| `started_at` | `*time.Time` | 可为空 | 任务开始处理时间 |
| `finished_at` | `*time.Time` | 可为空 | 任务结束时间，成功和失败都可写入 |

### 主要操作

- `CreateArchiveTask(task)`：创建新的归档任务。
- `SaveArchiveTask(task)`：保存任务完整状态，常用于状态流转、文件名和错误信息更新。
- `GetArchiveTaskByID(id)`：按任务 ID 查询任务状态。
- `GetLatestArchiveTaskByURL(rawURL)`：按 URL 查询最新任务，按 `created_at desc` 排序。
- `FindActiveArchiveTaskByURL(rawURL)`：按 URL 查找最新的活跃任务，活跃状态为 `pending` 或 `running`。
- `ListArchiveTasksByStatuses(statuses)`：按状态集合查询任务，并按 `created_at asc` 排序；用于恢复或处理待执行任务。

### 查询和索引设计

- `url` 有普通索引，支撑按 URL 查询最新任务和活跃任务。
- `status` 有普通索引，支撑按状态恢复队列或批量查询待处理任务。
- 当前没有 `(url, status, created_at)` 或 `(status, created_at)` 复合索引；如果归档任务量增大，`FindActiveArchiveTaskByURL` 和 `ListArchiveTasksByStatuses` 可能需要补充复合索引。
- 当前没有对 `url` 做唯一约束；代码通过查询最新任务和活跃任务实现幂等与复用，而不是依赖数据库唯一约束。

### 状态语义

`db.go` 中数据库查询明确把 `pending` 和 `running` 作为活跃任务状态。业务层应保证状态字符串的一致性，并在任务完成后写入最终状态、`file_name`、`error` 和 `finished_at` 等字段。

## archive_stats

HTML 归档统计表，用于保存当前归档目录中各个 URL 来源的 HTML 文件数量。这里的 URL 来源按归档目录中的域名目录聚合，例如 `example.com`。

总 HTML 文件数量不单独落一行，而是在查询时由所有来源的 `file_count` 求和得到。这样可以避免总数和分来源统计出现双写不一致。

| 字段 | Go 类型 | 约束/索引 | 说明 |
| --- | --- | --- | --- |
| `source` | `string` | 主键，长度 255 | URL 来源，当前按域名保存 |
| `file_count` | `int` | 非空，默认 0 | 该来源下已保存的 HTML 文件数量 |
| `created_at` | `time.Time` | GORM 自动维护 | 创建时间 |
| `updated_at` | `time.Time` | GORM 自动维护 | 更新时间 |

### 主要操作

- 查询统计信息：读取全部 `archive_stats` 行，按来源返回文件数量，并把 `file_count` 求和作为 HTML 文件总数。
- 刷新统计信息：扫描 `ARCHIVEFILELOACTION` 目录，跳过 `Temporary` 等非归档目录，统计每个来源目录下的 `.html` 文件数量，然后用扫描结果覆盖 `archive_stats` 表。
- 增量更新统计信息：当新增 HTML 文件成功完成索引并移动到来源目录后，对对应来源的 `file_count` 增加 1；如果目标文件本来已存在，则不增加总数。

### 后端接口

- `GET /api/archiveStats`：查询当前已入库的统计信息，返回总 HTML 文件数和各来源文件数。
- `POST /api/archiveStats/refresh`：刷新统计信息，扫描归档目录并用扫描结果覆盖统计表。

### 扫描规则

- 归档根目录下的一级目录名视为 URL 来源。
- 仅统计普通文件，且扩展名为 `.html` 的文件。
- `Temporary` 是上传中转目录，不计入统计。
- 根目录中未归属到来源目录的文件不计入统计；文件应在完成索引后移动到来源目录。

## 结构关系

当前数据库结构可以概括为：

```text
users
  id (PK)
  username (unique)
  password
  created_at
  updated_at

archive_tasks
  id (PK)
  url (index)
  domain
  status (index)
  file_name
  error
  external_task_id
  created_at
  updated_at
  started_at
  finished_at

archive_stats
  source (PK)
  file_count
  created_at
  updated_at
```
