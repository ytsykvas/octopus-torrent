# Frozen library schemas

Each `library-vN.sql` is a library as schema version N left it: the header lines
`-- application_id:` and `-- user_version:`, the schema as SQLite stored it, and rows in every
table. Two tests read it:
- `TestTheFrozenSchemaIsWhatItsMigrationsBuild` requires the schema text SQLite stores after migrations
  1..N to equal the fixture's `CREATE` statements. The structural check leaves `CHECK` contents out, so
  this test is what catches an edited released migration.
- `TestTheFrozenFirstSchemaOpensAtTheCurrentVersionWithEveryRow` builds a database from the file
  through the driver alone (`fixture` in `library_test.go`), opens it with `OpenLibrary`, and requires
  the current version with every row.

- A fixture is made **once**, by the build that released its version: `OpenLibrary` of that schema,
  its `sqlite_schema` SQL in creation order, then the rows. It is never regenerated from the
  migrations; editing a released migration therefore fails the text test instead of passing silently.
- Each statement ends with `;` at the end of a line, and no other line ends with `;`.
- Text, not a binary database, so a change is a readable diff.
- When migration N+1 lands, add `library-v(N+1).sql` the same way and extend the test to open every
  fixture; keep the older ones.

The data is synthetic: no owner's torrent, path or setting.
