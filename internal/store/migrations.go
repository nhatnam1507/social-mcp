package store

import (
	"embed"
	"sort"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	name string
	sql  string
}

// migrations are applied in file-name order; migration N sets user_version = N.
var migrations = loadMigrations()

func loadMigrations() []migration {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		panic(err)
	}
	var out []migration
	for _, e := range entries {
		b, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			panic(err)
		}
		out = append(out, migration{name: e.Name(), sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}
