# Search at scale — measured, not asserted

**CHRN-41 · measured 2026-09-05.**

The ticket's third `Done when` is *"search stays fast at 10× the current corpus"*.
The current corpus is ~17 real memos and 40 labelled eval memos, so 10× is a
few hundred rows — a number at which everything is fast, including a sequential
scan. Measuring at 10× would have proved nothing, so this measures at roughly
**300×** and asserts the shape of the plan rather than only the clock.

---

## The numbers

Postgres 16.15, GIN expression indexes from `0012_search`, one query through
`Store.Search` (`websearch_to_tsquery` → two `@@` predicates → `ts_headline` →
`ts_rank_cd` → sort), on an idle box.

| corpus | rows searched | hits | wall clock |
|---|---|---|---|
| 5 000 notes + 5 000 transcripts | 10 000 | 2 | **2.6 ms** |
| same | 10 000 | 2 | 2.9 ms |
| same | 10 000 | 2 | 4.9 ms |

Three runs, cold-ish (each run migrates the database down and back up, seeds,
and `ANALYZE`s). The spread is scheduling noise, not corpus effects.

## What is actually asserted in CI

`TestSearchUsesTheIndexAtScale` does **not** rest on the clock. A wall-clock
assertion alone is a flake on a busy runner, and — worse — it would still pass
if the planner quietly stopped using the index, because a sequential scan over
ten thousand rows is also fast. So the test asserts:

1. `EXPLAIN` of **the shipping statement**, not a paraphrase of it, mentions
   both `note_revisions_fts` and `transcripts_fts`;
2. neither `note_revisions` nor `transcripts` is sequentially scanned;
3. and, as a backstop against a plan collapse rather than a slow machine, the
   query finishes inside 2 s — three orders of magnitude of headroom.

The clock number above is logged by that test, not asserted by it.

## Why this stays true as the corpus grows

The index is a **GIN expression index**, so it is maintained by the server in
the same transaction as the write and cannot drift from its table. Growth costs
lookup time logarithmically in the number of distinct lexemes, not linearly in
the corpus — which is the property that makes "no Elasticsearch" the right call
for one operator's notes rather than a deferral of one.

The one thing that would change this picture is `ts_headline`, which runs on
the *matched* rows and re-parses their full text. It is bounded here by the
`LIMIT` being applied after ranking, so a query matching ten thousand rows
still only headlines the fifty it returns.

## The trap this measurement would catch

`to_tsvector(text)` — one argument — reads `default_text_search_config`, which
makes it `STABLE` and therefore unindexable. The two-argument form with a
literal `'english'` is `IMMUTABLE`. If a future edit to `searchSQL` drops the
`'english'`, or spells a `setweight` differently from the index, **the query
keeps returning exactly the right rows** by sequential scan and nothing fails.
That is the failure this file and `TestSearchUsesTheIndexAtScale` exist to
catch, and it is why the test explains the real statement rather than a copy.
