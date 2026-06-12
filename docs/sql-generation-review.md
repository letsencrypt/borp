# SQL Generation Review Guide

borp turns mapper metadata and Go values into SQL. This guide is grounded in the
code paths that already synthesize SQL. When reviewing those paths, first
classify each input as one of:

- SQL syntax emitted by borp.
- Values passed to `database/sql` as bind parameters.
- Caller-provided SQL fragments that are intentionally embedded in generated
  statements.

## Representative Code Anchors

Use these anchors as starting points for review, not as an exhaustive inventory.
Update this section when SQL synthesis moves or private helpers are renamed.

- Raw SQL entry points: `DbMap.Select`, `DbMap.SelectOne`, `DbMap.ExecContext`,
  and the matching `Transaction` methods accept caller-authored SQL.
- Generated DML and reads: the `TableMap` binding path, currently including
  `bindInsert`, `bindUpdate`, `bindDelete`, and `bindGet`, builds statements
  from mapper metadata and model values.
- Generated DDL and table maintenance: table creation, index creation and
  deletion, table drops, and truncate helpers build statements from table,
  column, index, and dialect metadata.
- Dialect boundary: `Dialect.QuoteField`, `QuotedTableForQuery`, `BindVar`,
  `QuerySuffix`, `AutoIncrInsertSuffix`, and DDL suffix methods define how
  logical names, bind placeholders, and dialect-specific syntax are rendered.
- Mapper metadata: `TableMap`, `ColumnMap`, `IndexMap`, and struct tags passed
  through `AddTable` or `AddTableWithName` feed generated SQL.
- Cached plans: `TableMap` caches insert, update, delete, and get plans and
  exposes `ResetSql` for mapper changes that require regeneration.
- Transaction helper names: savepoint helpers build SQL from caller-provided
  names and currently pass those names through dialect identifier quoting.

Some of these contracts are explicit in public APIs and comments, while others
are implicit in the way metadata is currently appended or quoted. Raw SQL entry
points, the `Dialect` interface, and `ResetSql` are explicit anchors; metadata
passed through `QuoteField` or appended directly is an implementation-derived
anchor. If a change depends on an implicit contract, prefer clarifying it with
documentation or tests instead of relying on this guide alone.

## Input Kinds

Model field values should normally be bind parameters. In generated DML, values
should flow through dialect bind placeholders and the argument list rather than
being copied into SQL text.

Identifier metadata should be rendered as identifiers through the active
dialect. This includes table, schema, column, index, and savepoint names.
Identifier quoting must preserve identifier context even when names contain
dialect delimiter characters.

Raw SQL fragments are caller-owned SQL. Query strings passed to raw SQL entry
points and metadata that is appended directly, such as defaults, index types, and
dialect suffixes, should be treated as SQL fragments unless the API is changed to
make them structured values.

## Invariants

- Generated identifiers must remain identifiers. Embedded quote characters,
  separators, comments, or suffixes must not escape the identifier context.
- Generated values should remain bind parameters unless an API explicitly
  accepts a SQL expression.
- Raw SQL escape hatches should be explicit in documentation and tests.
- Generated statement shape should match the API call. For example, inserting
  one model object should not create additional rows through mapper metadata.
- Transaction helpers should keep generated savepoint names within their
  intended SQL context.

Cached-plan inputs must be explicit. If an API input can change the generated
statement, it needs to be part of the cache key, the plan must be built per call,
or the API should avoid exposing that extra statement shape.

Dialect behavior should be tested per dialect. SQLite, MySQL, and PostgreSQL do
not quote or bind every construct the same way.

## Review Checklist

For every SQL-generation change, ask:

- Which bytes are SQL syntax and which values are bind parameters?
- Is each metadata field an identifier, a bind value, or an intentional raw SQL
  fragment?
- Can a struct tag, exported metadata field, or dialect hook influence those
  bytes?
- Are all identifier delimiters escaped for each affected dialect?
- Is the generated plan cached, and if so, what inputs are part of the cache key?
- Does the test fail if the generated SQL targets a different table, column,
  row count, statement, or predicate?

## Regression Expectations

Prefer focused deterministic regressions that inspect both behavior and generated
SQL when practical. Tests should execute through real borp public APIs when
possible.

Useful regression patterns include:

- A malicious quote character in every identifier position affected by the fix.
- A cached-plan test that first builds the unsafe plan and then exercises the
  later call shape that should differ.
- A single-statement integrity test for generated DML, such as proving only one
  intended row is inserted or updated.
- Dialect-specific tests for syntax that differs across SQLite, MySQL, and
  PostgreSQL.

Fuzzing can be useful for stable, pure invariants such as identifier quoting, but
it should supplement deterministic generated-SQL tests rather than replace them.

Run `go test ./...` after changing query generation.
