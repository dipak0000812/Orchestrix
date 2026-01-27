[1mdiff --git a/README.md b/README.md[m
[1mindex a40426b..359c7b3 100644[m
[1m--- a/README.md[m
[1m+++ b/README.md[m
[36m@@ -1,206 +1,196 @@[m
[31m-# Orchestrix[m
[32m+[m[32m[![GitHub Workflow Status (branch)](https://img.shields.io/github/actions/workflow/status/golang-migrate/migrate/ci.yaml?branch=master)](https://github.com/golang-migrate/migrate/actions/workflows/ci.yaml?query=branch%3Amaster)[m
[32m+[m[32m[![GoDoc](https://pkg.go.dev/badge/github.com/golang-migrate/migrate)](https://pkg.go.dev/github.com/golang-migrate/migrate/v4)[m
[32m+[m[32m[![Coverage Status](https://img.shields.io/coveralls/github/golang-migrate/migrate/master.svg)](https://coveralls.io/github/golang-migrate/migrate?branch=master)[m
[32m+[m[32m[![packagecloud.io](https://img.shields.io/badge/deb-packagecloud.io-844fec.svg)](https://packagecloud.io/golang-migrate/migrate?filter=debs)[m
[32m+[m[32m[![Docker Pulls](https://img.shields.io/docker/pulls/migrate/migrate.svg)](https://hub.docker.com/r/migrate/migrate/)[m
[32m+[m[32m![Supported Go Versions](https://img.shields.io/badge/Go-1.20%2C%201.21-lightgrey.svg)[m
[32m+[m[32m[![GitHub Release](https://img.shields.io/github/release/golang-migrate/migrate.svg)](https://github.com/golang-migrate/migrate/releases)[m
[32m+[m[32m[![Go Report Card](https://goreportcard.com/badge/github.com/golang-migrate/migrate/v4)](https://goreportcard.com/report/github.com/golang-migrate/migrate/v4)[m
[32m+[m
[32m+[m[32m# migrate[m
[32m+[m
[32m+[m[32m__Database migrations written in Go. Use as [CLI](#cli-usage) or import as [library](#use-in-your-go-project).__[m
[32m+[m
[32m+[m[32m* Migrate reads migrations from [sources](#migration-sources)[m
[32m+[m[32m   and applies them in correct order to a [database](#databases).[m
[32m+[m[32m* Drivers are "dumb", migrate glues everything together and makes sure the logic is bulletproof.[m
[32m+[m[32m   (Keeps the drivers lightweight, too.)[m
[32m+[m[32m* Database drivers don't assume things or try to correct user input. When in doubt, fail.[m
[32m+[m
[32m+[m[32mForked from [mattes/migrate](https://github.com/mattes/migrate)[m
[32m+[m
[32m+[m[32m## Databases[m
[32m+[m
[32m+[m[32mDatabase drivers run migrations. [Add a new database?](database/driver.go)[m
[32m+[m
[32m+[m[32m* [PostgreSQL](database/postgres)[m
[32m+[m[32m* [PGX v4](database/pgx)[m
[32m+[m[32m* [PGX v5](database/pgx/v5)[m
[32m+[m[32m* [Redshift](database/redshift)[m
[32m+[m[32m* [Ql](database/ql)[m
[32m+[m[32m* [Cassandra / ScyllaDB](database/cassandra)[m
[32m+[m[32m* [SQLite](database/sqlite)[m
[32m+[m[32m* [SQLite3](database/sqlite3) ([todo #165](https://github.com/mattes/migrate/issues/165))[m
[32m+[m[32m* [SQLCipher](database/sqlcipher)[m
[32m+[m[32m* [MySQL / MariaDB](database/mysql)[m
[32m+[m[32m* [Neo4j](database/neo4j)[m
[32m+[m[32m* [MongoDB](database/mongodb)[m
[32m+[m[32m* [CrateDB](database/crate) ([todo #170](https://github.com/mattes/migrate/issues/170))[m
[32m+[m[32m* [Shell](database/shell) ([todo #171](https://github.com/mattes/migrate/issues/171))[m
[32m+[m[32m* [Google Cloud Spanner](database/spanner)[m
[32m+[m[32m* [CockroachDB](database/cockroachdb)[m
[32m+[m[32m* [YugabyteDB](database/yugabytedb)[m
[32m+[m[32m* [ClickHouse](database/clickhouse)[m
[32m+[m[32m* [Firebird](database/firebird)[m
[32m+[m[32m* [MS SQL Server](database/sqlserver)[m
[32m+[m[32m* [RQLite](database/rqlite)[m
[32m+[m
[32m+[m[32m### Database URLs[m
[32m+[m
[32m+[m[32mDatabase connection strings are specified via URLs. The URL format is driver dependent but generally has the form: `dbdriver://username:password@host:port/dbname?param1=true&param2=false`[m
[32m+[m
[32m+[m[32mAny [reserved URL characters](https://en.wikipedia.org/wiki/Percent-encoding#Percent-encoding_reserved_characters) need to be escaped. Note, the `%` character also [needs to be escaped](https://en.wikipedia.org/wiki/Percent-encoding#Percent-encoding_the_percent_character)[m
[32m+[m
[32m+[m[32mExplicitly, the following characters need to be escaped:[m
[32m+[m[32m`!`, `#`, `$`, `%`, `&`, `'`, `(`, `)`, `*`, `+`, `,`, `/`, `:`, `;`, `=`, `?`, `@`, `[`, `]`[m
[32m+[m
[32m+[m[32mIt's easiest to always run the URL parts of your DB connection URL (e.g. username, password, etc) through an URL encoder. See the example Python snippets below:[m
[32m+[m
[32m+[m[32m```bash[m
[32m+[m[32m$ python3 -c 'import urllib.parse; print(urllib.parse.quote(input("String to encode: "), ""))'[m
[32m+[m[32mString to encode: FAKEpassword!#$%&'()*+,/:;=?@[][m
[32m+[m[32mFAKEpassword%21%23%24%25%26%27%28%29%2A%2B%2C%2F%3A%3B%3D%3F%40%5B%5D[m
[32m+[m[32m$ python2 -c 'import urllib; print urllib.quote(raw_input("String to encode: "), "")'[m
[32m+[m[32mString to encode: FAKEpassword!#$%&'()*+,/:;=?@[][m
[32m+[m[32mFAKEpassword%21%23%24%25%26%27%28%29%2A%2B%2C%2F%3A%3B%3D%3F%40%5B%5D[m
[32m+[m[32m$[m
[32m+[m[32m```[m
[32m+[m
[32m+[m[32m## Migration Sources[m
[32m+[m
[32m+[m[32mSource drivers read migrations from local or remote sources. [Add a new source?](source/driver.go)[m
[32m+[m
[32m+[m[32m* [Filesystem](source/file) - read from filesystem[m
[32m+[m[32m* [io/fs](source/iofs) - read from a Go [io/fs](https://pkg.go.dev/io/fs#FS)[m
[32m+[m[32m* [Go-Bindata](source/go_bindata) - read from embedded binary data ([jteeuwen/go-bindata](https://github.com/jteeuwen/go-bindata))[m
[32m+[m[32m* [pkger](source/pkger) - read from embedded binary data ([markbates/pkger](https://github.com/markbates/pkger))[m
[32m+[m[32m* [GitHub](source/github) - read from remote GitHub repositories[m
[32m+[m[32m* [GitHub Enterprise](source/github_ee) - read from remote GitHub Enterprise repositories[m
[32m+[m[32m* [Bitbucket](source/bitbucket) - read from remote Bitbucket repositories[m
[32m+[m[32m* [Gitlab](source/gitlab) - read from remote Gitlab repositories[m
[32m+[m[32m* [AWS S3](source/aws_s3) - read from Amazon Web Services S3[m
[32m+[m[32m* [Google Cloud Storage](source/google_cloud_storage) - read from Google Cloud Platform Storage[m
[32m+[m
[32m+[m[32m## CLI usage[m
[32m+[m
[32m+[m[32m* Simple wrapper around this library.[m
[32m+[m[32m* Handles ctrl+c (SIGINT) gracefully.[m
[32m+[m[32m* No config search paths, no config files, no magic ENV var injections.[m
[32m+[m
[32m+[m[32m__[CLI Documentation](cmd/migrate)__[m
 [m
[31m-Orchestrix is a backend job orchestration service built in Go that manages asynchronous task execution with explicit lifecycle control, retries, and observability.[m
[32m+[m[32m### Basic usage[m
 [m
[31m-The system is designed as a **single-binary monolith** with strong internal boundaries, prioritizing correctness, debuggability, and operational clarity over premature distribution.[m
[32m+[m[32m```bash[m
[32m+[m[32m$ migrate -source file://path/to/migrations -database postgres://localhost:5432/database up 2[m
[32m+[m[32m```[m
 [m
[31m----[m
[32m+[m[32m### Docker usage[m
 [m
[31m-## Project Status[m
[32m+[m[32m```bash[m
[32m+[m[32m$ docker run -v {{ migration dir }}:/migrations --network host migrate/migrate[m
[32m+[m[32m    -path=/migrations/ -database postgres://localhost:5432/database up 2[m
[32m+[m[32m```[m
 [m
[31m-🚧 **Under Active Development**[m
[32m+[m[32m## Use in your Go project[m
 [m
[31m-**Current Phase:** Foundation  [m
[31m-The core runtime shell, configuration system, and architectural groundwork are complete.  [m
[31m-Domain logic and execution engine are under active development.[m
[32m+[m[32m* API is stable and frozen for this release (v3 & v4).[m
[32m+[m[32m* Uses [Go modules](https://golang.org/cmd/go/#hdr-Modules__module_versions__and_more) to manage dependencies.[m
[32m+[m[32m* To help prevent database corruptions, it supports graceful stops via `GracefulStop chan bool`.[m
[32m+[m[32m* Bring your own logger.[m
[32m+[m[32m* Uses `io.Reader` streams internally for low m