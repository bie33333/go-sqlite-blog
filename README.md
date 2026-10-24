# Go-SQLite-Blog

Go-SQLite-Blog is a lightweight blogging platform written in Go (Golang). It uses SQLite for data storage, making it easy to set up without the need for a separate database server.

> 说明：本项目仍在开发中。README 下面的 Features 列表是长期目标，实际已实现的功能以「当前实现状态」一节为准。

## 当前实现状态（最小可用博客）

目前已经可以「注册 / 登录 / 发文 / 看文章 / 评论 / 管理员审核」。

### 已实现路由

| 方法 | 路径 | 说明 | 权限 |
| --- | --- | --- | --- |
| GET | `/health` | 健康检查 | 公开 |
| GET | `/time-consuming` | 演示用慢接口（sleep 5s） | 公开 |
| GET | `/register` | 注册页 | 公开 |
| POST | `/register` | 提交注册（角色固定为 author） | 公开 |
| GET | `/login` | 登录页 | 公开 |
| POST | `/login` | 登录（成功后写入签名 cookie session） | 公开 |
| POST | `/logout` | 退出登录 | 公开 |
| GET | `/` | 文章列表（空库显示“暂无文章”） | 公开 |
| GET | `/post/{id}` | 文章详情 + 已通过审核的评论 | 公开 |
| GET | `/post/new` | 发文表单（未登录 302 到 `/login`） | 登录 |
| POST | `/post` | 发布文章 | 登录 |
| POST | `/post/{id}/comment` | 发表评论（`approved=0`，待审核） | 登录 |
| GET | `/admin` | 管理后台：评论审核 + 文章管理 | admin |
| POST | `/admin/comments/{id}/approve` | 通过评论审核（`approved` 置 1） | admin |
| POST | `/admin/comments/{id}/delete` | 删除评论 | admin |
| POST | `/admin/articles/{id}/delete` | 删除文章（级联清理评论/修订/标签/点赞） | admin |

未登录访问 admin 路由会 302 到 `/login`；已登录但非 admin 返回 **403** 页面。

### 角色与管理员

| id | 名称 | 说明 |
| --- | --- | --- |
| 1 | admin | 管理员，可访问 `/admin` |
| 2 | editor | 预留，暂无专属权限 |
| 3 | author | **注册用户的默认角色**，可登录/发文/评论 |

角色以**数据库中的 `users.role_id` 为准**（不是登录时写进 session 的值），所以提升/降级后立即生效，无需重新登录。

如何得到第一个管理员（注册的都是 author，没有自助入口）：

```bash
# 1. 先正常注册一个账号，例如 alice
# 2. 用它提升为管理员（启动时执行，然后继续提供服务）
./go-sqlite-blog --promote-admin=alice
#   或：PROMOTE_ADMIN=alice ./go-sqlite-blog
# 3. 用 alice 登录，导航栏会出现「管理」，进入 /admin
```

用户名不存在时启动会报错退出（不会静默失败）。

### 数据库初始化与种子数据

- 服务启动时会执行项目根目录的 `tables.sql`（全部是 `CREATE TABLE IF NOT EXISTS`，可重复执行）。
- `tables.sql` 末尾会幂等写入角色种子数据：
  `INSERT OR IGNORE INTO roles ... (1,'admin'),(2,'editor'),(3,'author')`。
  这样 `users.role_id DEFAULT 1` 的外键不会再失败，无需手动插 `roles`。
- 重置数据库：
  ```bash
  ./go-sqlite-blog --database-reset=true     # 或 DATABASE_RESET=true
  ```
  重置会删除数据库文件并重新建表 + 写入种子数据。

### 启动

```bash
go build -o go-sqlite-blog
./go-sqlite-blog
# 打开 http://localhost:8080
```

必须在**项目根目录**启动，因为 `tables.sql` 与 `static/templates` 都使用相对路径。

可用环境变量（优先级高于 `config.yaml`）：`PORT`、`DATABASE_DRIVER`、`DATABASE_SOURCE`、`DATABASE_RESET`、`DATABASE_LOG_QUERIES`、`ERRORS_IN_RESPONSE`、`IP_RATE_LIMIT`、`BURST_RATE_LIMIT`、`SESSION_SECRET`、`PROMOTE_ADMIN`。

`SESSION_SECRET` 用于签名登录 cookie；未设置时每次启动随机生成，因此重启后需要重新登录（不设置也能用，但重启会掉登录态）。

### 已知限制

- `articles` 表没有作者字段：作者信息保存在 `article_revisions` 的第一条记录（创建时写入的初版，`edited_by`）中，列表/详情页据此显示作者。
- 文章正文当前按纯文本展示（`white-space: pre-wrap`），未渲染 Markdown。
- 评论一律先进入「待审核」，未通过审核前只有管理员能在详情页看到；作者本人提交后会看到「等待审核」的提示。
- `users.role_id` 的建表默认值仍是 1（admin）。应用层注册已固定写 3（author），但如果你绕过应用直接 `INSERT` 用户，记得显式指定 `role_id`。
- 管理后台表单没有 CSRF token，依赖 session cookie 的 `SameSite=Lax` 阻止跨站 POST。若要对外部署，建议补 CSRF token。
- 分类 / 标签 / 媒体 / 静态页面 / 搜索 / 分页等表结构已存在，但尚未实现对应功能。

## Table of Contents

- [Features](#features)
- [Demo](#demo)
- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
  - [Installation](#installation)
- [Usage](#usage)
  - [Configuration](#configuration)
  - [Running the Application](#running-the-application)
- [Project Structure](#project-structure)
- [Database Schema](#database-schema)
- [Contributing](#contributing)
- [License](#license)

## Features

- Admin Friendly: Easy to use, manage and backup - there is just the executable and the database file
- User Management: Support for multiple user roles (admin, editor, author)
- Article Management: Create, edit, and delete blog posts with Markdown support
- Comments: Enable readers to leave comments on posts (with moderation options)
- Categories and Tags: Organize posts using categories and tags
- Media Uploads: Upload and manage images and other media files
- Static Pages: Create static pages like "About Us" or "Contact"
- Search Functionality: Full-text search on articles
- Pagination: Navigate through posts with ease

## Demo

Check out a live demo of Go-SQLite-Blog [here](#). - _Coming Soon_

## Getting Started

### With Docker

1. Install Docker
2. Run `docker run -it --rm -v ~/.ssh:/home/developer/.ssh -v $(pwd):/workspace -w /workspace andreheber/go-sqlite-blog-dev:latest`

### On your local machine

#### Prerequisites

- Go 1.22 or higher
- Git

#### Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/AndreHeber/go-sqlite-blog.git
   cd go-sqlite-blog
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Build the application:
   ```bash
   go build -o go-sqlite-blog
   ```

## Usage

### Configuration

Before running the application, you can configure settings in the `config.yaml` file:

```yaml
log_level: DEBUG # DEBUG, INFO, WARN, ERROR
port: 8080
errors_in_response: true
database:
  driver: sqlite3
  source: ./blog.db
  reset: false
  log_queries: true
ip_rate_limit: 10
burst_rate_limit: 20
```

### Running the Application

1. Start the server

```bash
./go-sqlite-blog
```

2. Access the application

    Open your web browser and navigate to http://localhost:8080.

## Project Structure

```
go-sqlite-blog/
├── main.go                  # 入口 + setupRouter 路由注册
├── tables.sql               # 建表语句 + 角色种子数据
├── config.yaml              # 配置
├── config/                  # 配置加载（文件 / 环境变量 / flag）
├── db/                      # 数据库连接与建表
├── handlers/                # HTTP handlers
│   ├── article.go           # 文章列表 / 详情 / 发文 / 评论
│   ├── admin.go             # 管理后台：审核评论、删除评论/文章
│   ├── login.go             # 登录 / 登出
│   ├── register.go          # 注册
│   ├── templates.go         # 统一模板加载与渲染
│   └── session.go           # session 读取辅助
├── middleware/              # Adapter、session、RequireLogin
├── models/                  # 模型与 SQL（users 使用 go:embed）
│   └── users/roles.go       # 角色常量、GetRoleID、PromoteToAdmin
├── static/templates/        # layout.html + 各页面模板
└── go.mod
```

## Database Schema

Go-SQLite-Blog uses SQLite with the following tables:

```sql
users: Manage user accounts and roles.
articles: Store blog posts with metadata.
comments: Allow users to comment on articles.
categories: Organize articles into categories.
article_categories: Link articles to categories (many-to-many relationship).
tags: Tag articles for detailed classification.
article_tags: Link articles to tags (many-to-many relationship).
media: Manage uploaded media files.
pages: Create static pages.
settings: Store application settings.
```

Refer to the schema.sql file for detailed definitions.

## Contributing

Contributions are welcome! Please follow these steps:

1. Fork the repository

```bash
git clone https://github.com/yourusername/go-sqlite-blog.git
cd go-sqlite-blog
```

2. Create a feature branch

```bash
git checkout -b feature/YourFeature
```

3. Make your changes

4. Run the tests

```bash
go test ./...
```

5. Commit your changes

```bash
git commit -m 'Add YourFeature'
```

6. Push to the branch

```bash
git push origin feature/YourFeature
```

7. Open a pull request

## License

This project is licensed under the MIT License. See the LICENSE file for details.