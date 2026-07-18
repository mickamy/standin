package model

import "time"

// User is a regular fixture target.
type User struct {
	ID        int64
	Name      string `fake:"{firstname}"`
	email     string
	CreatedAt time.Time
	Profile   Profile
	Note      *string
}

// Profile is referenced by other structs and is also a target.
type Profile struct {
	Bio string
}

// Admin embeds Profile.
type Admin struct {
	Profile

	Level int
}

// Secret is excluded from generation.
//
//standin:ignore
type Secret struct {
	Token string
}

// SpacedIgnore is excluded too; the directive has a space after the marker.
//
// standin:ignore
type SpacedIgnore struct {
	A int
}

// NotIgnored mentions standin:ignoreXYZ, which is not our directive.
type NotIgnored struct {
	A int
}

// Pair is generic and skipped with a warning.
type Pair[T any] struct {
	A T
	B T
}

// Alias must not get its own fixture.
type Alias = User

// Status is a named basic type, not a struct.
type Status string

// hidden is unexported.
type hidden struct {
	X int
}
