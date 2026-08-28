// Package migrations embeds the ASR service's SQL migrations.
//
// SEPARATE FROM CHRONICLE'S, and that is the point. The repo-root
// `migrations` package is `//go:embed *.sql` over CHRONICLE's migrations, so a
// second service reusing it would apply Chronicle's schema — tier1, tier2,
// users, memos — to the `asr` database. These are different services with
// different stores, and the embed is where that either holds or quietly stops
// holding.
package migrations

import "embed"

// FS holds every *.up.sql / *.down.sql migration in this directory.
//
//go:embed *.sql
var FS embed.FS
