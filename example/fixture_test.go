package example_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/mickamy/standin/example/fixture"
	"github.com/mickamy/standin/example/model"
)

func TestUser(t *testing.T) {
	t.Parallel()

	u := fixture.User()

	if u.Name == "" {
		t.Error("Name is empty, want a fake value")
	}

	if u.Email == "" {
		t.Error("Email is empty, want a fake value")
	}

	if u.Age < 18 || u.Age > 65 {
		t.Errorf("Age = %d, want within [18, 65]", u.Age)
	}

	if u.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero, want a fake date")
	}

	if u.Bio != nil {
		t.Errorf("Bio = %v, want nil", u.Bio)
	}
}

func TestUserSetters(t *testing.T) {
	t.Parallel()

	u := fixture.User(
		func(m *model.User) { m.Name = "Alice" },
		func(m *model.User) { m.Email = "alice@example.com" },
	)

	if u.Name != "Alice" {
		t.Errorf("Name = %q, want %q", u.Name, "Alice")
	}

	if u.Email != "alice@example.com" {
		t.Errorf("Email = %q, want %q", u.Email, "alice@example.com")
	}
}

func TestSession(t *testing.T) {
	t.Parallel()

	s := fixture.Session()

	if s.ID == uuid.Nil {
		t.Error("ID is the nil UUID, want a fake value")
	}

	if s.ExpiresAt.IsZero() {
		t.Error("ExpiresAt is zero, want a fake date")
	}
}

func TestArticle(t *testing.T) {
	t.Parallel()

	a := fixture.Article()

	if a.Title == "" {
		t.Error("Title is empty, want a fake value")
	}

	if a.Author.Name == "" {
		t.Error("Author.Name is empty, want a nested fixture value")
	}

	if a.Tags != nil {
		t.Errorf("Tags = %v, want nil", a.Tags)
	}

	if a.PublishedAt.IsZero() {
		t.Error("PublishedAt is zero, want a fake date")
	}
}
