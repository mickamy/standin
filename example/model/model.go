package model

import (
	"time"

	"github.com/google/uuid"
)

// User is an application user.
type User struct {
	ID        int64
	Name      string
	Email     string
	Age       int `fake:"{number:18,65}"`
	Bio       *string
	CreatedAt time.Time
}

// Session is a signed-in browser session.
type Session struct {
	ID        uuid.UUID
	UserID    int64
	ExpiresAt time.Time
}

// Status is the publication status of an article.
type Status string

// Article is a post written by a user.
type Article struct {
	ID          int64
	Slug        string `fake:"???-####"`
	Title       string
	Body        string `fake:"{sentence:10}"`
	Status      Status `fake:"{word}"`
	Author      User
	Tags        []string
	PublishedAt time.Time
}
