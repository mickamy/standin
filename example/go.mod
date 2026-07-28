module github.com/mickamy/standin/example

go 1.25.0

tool github.com/mickamy/standin

replace github.com/mickamy/standin => ../

require (
	github.com/brianvoe/gofakeit/v7 v7.15.0
	github.com/google/uuid v1.6.0
)

require (
	github.com/mickamy/standin v0.0.0-00010101000000-000000000000 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
)
