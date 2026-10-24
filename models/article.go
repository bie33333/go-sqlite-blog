package models

// Article 对应 tables.sql 中的 articles 表。
// 注意：当前表结构只有 id/title/content/created_at，没有作者字段。
type Article struct {
	ID        int64
	Title     string
	Content   string
	Excerpt   string
	CreatedAt string
}

// Comment 对应 tables.sql 中的 comments 表。
type Comment struct {
	ID         int64
	ArticleID  int64
	UserID     *int64
	AuthorName string
	Content    string
	CreatedAt  string
	Approved   bool
}
