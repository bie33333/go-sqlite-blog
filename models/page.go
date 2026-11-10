package models

type Page struct {
	ID           int64
	Title        string
	Slug         string
	Content      string
	Status       string
	AllowScripts bool
	CreatedAt    string
	UpdatedAt    string
}
