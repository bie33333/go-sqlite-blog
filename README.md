# Go-SQLite-Blog

Go-SQLite-Blog is a lightweight blogging platform written in Go (Golang). It uses SQLite for data storage, making it easy to set up without the need for a separate database server.

> 说明：本项目仍在开发中。README 下面的 Features 列表是长期目标，实际已实现的功能以「当前实现状态」一节为准。

## 当前实现状态（最小可用博客）

目前已经可以「注册 / 登录 / 发文 / 看文章 / 评论 / 管理员审核」。

### 身份展示与管理入口

- 登录后导航栏会显示 `用户名 + 角色徽章`（管理员 / 编辑 / 作者），未登录不显示。
- **管理员**额外看到：
  - 导航栏「管理」→ `/admin`（评论审核 + 文章管理）
  - 文章详情页顶部的「管理后台」「删除本文」按钮
  - 详情页每条评论的「通过」「删除」按钮（含待审核评论，页面会提示这是管理员视角）
- 普通作者只能看到已通过审核的评论，也没有任何管理按钮。
- 角色徽章的颜色：管理员=靛蓝、编辑=绿色、作者=灰色。

### Markdown 与图片

文章正文使用 Markdown 编写，服务端用 [goldmark](https://github.com/yuin/goldmark) 渲染（GFM 扩展）：

- 支持：标题、粗体/斜体/删除线、行内代码与代码块（带语言高亮 class）、有序/无序列表、引用、表格、链接、图片、任务列表、分隔线。
- 单个换行会渲染为 `<br>`（`WithHardWraps`），符合博客书写习惯。
- **原始 HTML 标签会被丢弃**（goldmark 默认不开启 `WithUnsafe`），渲染结果可以安全地作为 HTML 输出，无需额外的 XSS 过滤。
- 首页列表的摘要由 Markdown 渲染后剥离标签得到，因此不会出现 `#`、`**` 之类的标记。
- 评论内容仍是**纯文本**展示（保留换行），不渲染 Markdown。

图片有两种写法，都会被渲染成 `<img>`：

```markdown
![说明](/uploads/abc123.png)          <!-- 本地上传的图片 -->
![说明](https://example.com/a.png)    <!-- 外链图片 -->
```

**从网页/AI 直接粘贴的内容里的图片也能显示**，三种常见形态都支持：

```html
<img src="https://example.com/a.png">                              <!-- 原始 HTML -->
<img src="https://example.com/a.png" style="zoom: 5%;">            <!-- 带样式的 HTML -->
```
```markdown
[image](https://example.com/a.png)   <!-- ChatGPT/搜索引擎复制出来的伪链接 -->
```

实现方式是**白名单抽取**，而不是打开 goldmark 的 `WithUnsafe`（那等于允许任意 HTML）：

1. 渲染前把 `<img ...>` 与 `[image](url)` 抽出来，换成占位符；
2. 按白名单重建标签，只保留 `src` / `alt` / `title` / `width` / `height` 与尺寸类样式；
3. 渲染后把占位符替换回重建好的标签。

因此被保留下来的只有图片，`<script>`、`<iframe>`、`onerror=` 等依然会被丢弃。

| 属性 | 处理方式 |
| --- | --- |
| `src` | 只允许 `http(s)://`、`/`、`./`；拒绝 `javascript:`、`data:`、`vbscript:`、`//host` |
| `alt` | 原样保留；缺失时用文件名兜底（长 hash 文件名降级为「图片」） |
| `title` | 保留 |
| `width` / `height` | 只接受纯数字 |
| `style` | 白名单：`width` `height` `max-width` `max-height` `object-fit` `border-radius` |
| **`style` 中的 `zoom`** | **转换成等价的 `width` 百分比**（见下） |
| `style` 中的 `background` / `position` 等 | 丢弃（`background: url()` 可引入外部资源） |
| `onerror` / `onclick` 等事件属性 | 丢弃 |
| `<script>` / `<iframe>` 等其它标签 | 丢弃（保持原有安全模型） |

### 控制图片大小

三种写法都支持，最终都输出成标准的 `style="width:..."`：

```html
<!-- 1) HTML + zoom：Typora 和从网页/AI 复制的内容长这样 -->
<img src="https://example.com/a.png" alt="封面" style="zoom:10%;">
```
```markdown
<!-- 2) Markdown + 尺寸属性（推荐，写起来最省事） -->
![封面](https://example.com/a.png){width=40%}
![封面](https://example.com/a.png){zoom=40%}
![封面](https://example.com/a.png){width=400, height=200}
![封面](https://example.com/a.png){w=50% h=30%}
```
```markdown
<!-- 3) 外链图片直接带 HTML 尺寸属性 -->
<img src="https://example.com/a.png" width="400" height="200">
```

换算规则：

| 写法 | 输出 |
| --- | --- |
| `zoom:10%` / `{zoom=10%}` | `width:10%` |
| `{zoom=0.5}` | `width:50%` |
| `{zoom=2}` | `width:200%` |
| `{width=400}` | `width:400px`（纯数字按 px） |
| `{width=40%, height=200}` | `width:40%;height:200px` |
| `zoom` 与显式 `width` 同时出现 | 以显式 `width` 为准 |
| `zoom` ≤ 0 或 > 500% | 丢弃（防止写出荒唐尺寸） |

**为什么要转换而不是直接用 `zoom`**：CSS 的 `zoom` 是非标准属性
（Firefox 直到 126 才支持），而且它缩放的是整个元素盒模型，行为不稳定。
改成 `width` 百分比是标准 CSS，各浏览器一致；配合 `.markdown img { max-width: 100%; height: auto; }`
还能保证只写 `width` 时图片等比缩放、不溢出容器。

> 提示：`{...}` 必须**紧贴**在图片语法后面，中间不能有空格。
> 花括号里的键不认识（如 `{color=red}`）时，整段会当作普通正文保留。

> 提示：`[image](url)` 只在链接文字恰为 `image` / `图片` / `图像` 时才转成图片，
> 普通链接（如 `[点这里](url)`）不受影响。

渲染出的 `<img>` 自动带上 `loading="lazy"`、`decoding="async"`、`referrerpolicy="no-referrer"`：

- 长文章里图片延迟加载，不阻塞首屏；
- 不发 Referer，绕过多数图床的防盗链，外链图更容易显示出来。

`.markdown img { max-width: 100%; height: auto; }`，大图会自适应容器宽度，不会撑破布局。

**上传图片**：在 `/post/new` 编辑器里点「插入图片」选择文件即可。上传走 `fetch` 异步进行，
成功后自动把 `![文件名](/uploads/xxx.png)` 插到光标位置，**不会丢失已输入的正文**。

上传规则：

| 项目 | 限制 |
| --- | --- |
| 权限 | 需登录 |
| 大小 | ≤ 5 MB |
| 类型 | PNG / JPEG / GIF / WebP（按**文件头**判定，不信任扩展名与 Content-Type） |
| 落盘名 | 服务端随机生成（16 字节 hex + 扩展名），不使用用户文件名 |
| 目录 | `static/uploads/`，映射到 `/uploads/`，已加入 `.gitignore` |
| 记录 | 同时写入 `media` 表（`file_name` 存原始名，`file_path` 存 URL） |

`/uploads/` 只服务文件，**禁止目录列表**，并且 `/static/templates/` 不会对外暴露。

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
| POST | `/uploads` | 上传文章图片，JSON 返回 `url` / `markdown` | 登录 |
| GET | `/uploads/{file}` | 访问上传的图片（`static/uploads/`，不列目录） | 公开 |
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
- 评论一律先进入「待审核」，未通过审核前只有管理员能在详情页看到；作者本人提交后会看到「等待审核」的提示。
- `users.role_id` 的建表默认值仍是 1（admin）。应用层注册已固定写 3（author），但如果你绕过应用直接 `INSERT` 用户，记得显式指定 `role_id`。
- 管理后台表单没有 CSRF token，依赖 session cookie 的 `SameSite=Lax` 阻止跨站 POST。若要对外部署，建议补 CSRF token。
- 图片只有上传，**没有图片管理页**：删除图片需要手动删掉 `static/uploads/` 下的文件（`media` 表的记录也会残留）。
- 上传文件名由服务端随机生成，原始文件名只用于 `alt` 文本和 `media.file_name`，因此同名文件不会互相覆盖。
- 分类 / 标签 / 静态页面 / 搜索 / 分页等表结构已存在，但尚未实现对应功能。

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
│   ├── markdown.go          # Markdown 渲染（goldmark）与摘要提取
│   ├── embedded_image.go    # 正文中粘贴的 HTML <img> 白名单抽取与重建
│   ├── upload.go            # 图片上传
│   ├── admin.go             # 管理后台：审核评论、删除评论/文章
│   ├── login.go             # 登录 / 登出
│   ├── register.go          # 注册
│   ├── templates.go         # 统一模板加载与渲染
│   └── session.go           # session 读取辅助
├── middleware/              # Adapter、session、RequireLogin、静态文件
├── models/                  # 模型与 SQL（users 使用 go:embed）
│   └── users/roles.go       # 角色常量、GetRoleID、PromoteToAdmin
├── static/templates/        # layout.html + 各页面模板
├── static/uploads/          # 上传的图片（已 gitignore）
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