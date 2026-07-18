package model

import "time"

// User is an application user.
type User struct {
	ID        int64
	Name      string
	Email     string
	Age       int `fake:"{number:18,65}"`
	Bio       *string
	CreatedAt time.Time
}

// Article is a post written by a user.
type Article struct {
	ID          int64
	Title       string
	Body        string `fake:"{sentence:10}"`
	Author      User
	Tags        []string
	PublishedAt time.Time
}
