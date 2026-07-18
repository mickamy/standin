package model

import "time"

// User is a fixture target.
type User struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

// Post is a fixture target used to test -exclude.
type Post struct {
	Title string
}

// Pair is generic and skipped with a warning.
type Pair[T any] struct {
	A T
	B T
}
