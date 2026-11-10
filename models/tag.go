package models

type Tag struct {
	ID   int64
	Name string
	Slug string
}

type Breadcrumb struct {
	Title string
	URL   string
}
