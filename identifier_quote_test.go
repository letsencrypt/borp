package borp

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestSqliteDialectEscapesIdentifierQuotes(t *testing.T) {
	dialect := SqliteDialect{}
	got := dialect.QuoteField(`fo"o`)
	want := `"fo""o"`
	if got != want {
		t.Fatalf("QuoteField() = %q, want %q", got, want)
	}
	got = dialect.QuotedTableForQuery("", `ta"ble`)
	want = `"ta""ble"`
	if got != want {
		t.Fatalf("QuotedTableForQuery() = %q, want %q", got, want)
	}
}

func TestSqliteQuotedTableNameCannotRewriteUpdateTarget(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE victim (id integer primary key, value text, admin integer)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("INSERT INTO victim (id, value, admin) VALUES (1, 'unchanged', 0)")
	if err != nil {
		t.Fatal(err)
	}

	type row struct {
		ID    int64  `db:"ID"`
		Value string `db:"Value"`
	}

	dbmap := &DbMap{Db: db, Dialect: SqliteDialect{}}
	injectedTable := `victim" SET admin = 1 WHERE ? <> ? -- `
	dbmap.AddTableWithName(row{}, injectedTable).SetKeys(false, "ID")

	_, err = dbmap.Update(context.Background(), &row{ID: 1, Value: "unused"})
	if err == nil {
		t.Fatal("Update succeeded for escaped malicious table name")
	}

	var admin int
	err = db.QueryRow("SELECT admin FROM victim WHERE id = 1").Scan(&admin)
	if err != nil {
		t.Fatal(err)
	}
	if admin != 0 {
		t.Fatalf("victim.admin = %d, want 0", admin)
	}
}

type identifierCapturedExec struct {
	query string
	args  []driver.NamedValue
}

var identifierCaptureState = struct {
	sync.Mutex
	registered sync.Once
	execs      []identifierCapturedExec
}{}

type identifierCaptureDriver struct{}

func (identifierCaptureDriver) Open(string) (driver.Conn, error) {
	return identifierCaptureConn{}, nil
}

type identifierCaptureConn struct{}

func (identifierCaptureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("identifier capture driver does not prepare statements")
}

func (identifierCaptureConn) Close() error {
	return nil
}

func (identifierCaptureConn) Begin() (driver.Tx, error) {
	return nil, errors.New("identifier capture driver does not begin transactions")
}

func (identifierCaptureConn) ExecContext(
	_ context.Context,
	query string,
	args []driver.NamedValue,
) (driver.Result, error) {
	argsCopy := append([]driver.NamedValue(nil), args...)

	identifierCaptureState.Lock()
	defer identifierCaptureState.Unlock()
	identifierCaptureState.execs = append(identifierCaptureState.execs, identifierCapturedExec{
		query: query,
		args:  argsCopy,
	})
	return driver.RowsAffected(0), nil
}

func newIdentifierCaptureDbMap(t *testing.T, dialect Dialect) *DbMap {
	t.Helper()

	identifierCaptureState.registered.Do(func() {
		sql.Register("borp_identifier_capture", identifierCaptureDriver{})
	})

	identifierCaptureState.Lock()
	identifierCaptureState.execs = nil
	identifierCaptureState.Unlock()

	db, err := sql.Open("borp_identifier_capture", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeErr := db.Close()
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	})

	return &DbMap{Db: db, Dialect: dialect}
}

func identifierCapturedExecs() []identifierCapturedExec {
	identifierCaptureState.Lock()
	defer identifierCaptureState.Unlock()
	return append([]identifierCapturedExec(nil), identifierCaptureState.execs...)
}

func requireIdentifierCapturedQuery(t *testing.T, want string) {
	t.Helper()
	execs := identifierCapturedExecs()
	if len(execs) != 1 {
		t.Fatalf("expected one captured exec, got %d: %+v", len(execs), execs)
	}
	if execs[0].query != want {
		t.Fatalf("generated %q, want %q", execs[0].query, want)
	}
}

func TestUpdateQuotesIdentifierMetadata(t *testing.T) {
	type updatedRow struct {
		ID    int64  `db:"ID"`
		Value string `db:"Value"`
	}

	tests := []struct {
		name    string
		dialect Dialect
		schema  string
		table   string
		want    string
	}{
		{
			name:    "sqlite",
			dialect: SqliteDialect{},
			schema:  `sche"ma`,
			table:   `security"rows`,
			want:    `update "security""rows" set "Value"=? where "ID"=?;`,
		},
		{
			name:    "postgres",
			dialect: PostgresDialect{},
			schema:  `sche"ma`,
			table:   `security"rows`,
			want:    `update "sche""ma"."security""rows" set "Value"=$1 where "ID"=$2;`,
		},
		{
			name:    "mysql",
			dialect: MySQLDialect{Engine: "InnoDB", Encoding: "UTF8"},
			schema:  "sche`ma",
			table:   "security`rows",
			want:    "update `sche``ma`.`security``rows` set `Value`=? where `ID`=?;",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dbmap := newIdentifierCaptureDbMap(t, tc.dialect)
			table := dbmap.AddTableWithNameAndSchema(updatedRow{}, tc.schema, tc.table)
			table.SetKeys(true, "ID")

			_, err := dbmap.Update(context.Background(), &updatedRow{ID: 1, Value: "unused"})
			if err != nil {
				t.Fatal(err)
			}

			requireIdentifierCapturedQuery(t, tc.want)
		})
	}
}

func TestCreateIndexQuotesIdentifierMetadata(t *testing.T) {
	type indexedRow struct {
		ID int64 `db:"ID"`
	}

	tests := []struct {
		name      string
		dialect   Dialect
		schema    string
		table     string
		indexName string
		indexType string
		want      string
	}{
		{
			name:      "sqlite",
			dialect:   SqliteDialect{},
			schema:    `sche"ma`,
			table:     `security"rows`,
			indexName: `idx"name`,
			want:      `create index "idx""name" on "security""rows" ("ID");`,
		},
		{
			name:      "postgres",
			dialect:   PostgresDialect{},
			schema:    `sche"ma`,
			table:     `security"rows`,
			indexName: `idx"name`,
			indexType: "btree",
			want:      `create index "idx""name" on "sche""ma"."security""rows" using btree ("ID");`,
		},
		{
			name:      "mysql",
			dialect:   MySQLDialect{Engine: "InnoDB", Encoding: "UTF8"},
			schema:    "sche`ma",
			table:     "security`rows",
			indexName: "idx`name",
			indexType: "Btree",
			want:      "create index `idx``name` on `sche``ma`.`security``rows` (`ID`) using Btree;",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dbmap := newIdentifierCaptureDbMap(t, tc.dialect)
			table := dbmap.AddTableWithNameAndSchema(indexedRow{}, tc.schema, tc.table)
			table.SetKeys(false, "ID")
			table.AddIndex(tc.indexName, tc.indexType, []string{"ID"})

			err := dbmap.CreateIndex(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			requireIdentifierCapturedQuery(t, tc.want)
		})
	}
}

func TestDropIndexQuotesIdentifierMetadata(t *testing.T) {
	type indexedRow struct {
		ID int64 `db:"ID"`
	}

	tests := []struct {
		name      string
		dialect   Dialect
		schema    string
		table     string
		indexName string
		indexType string
		want      string
	}{
		{
			name:      "sqlite",
			dialect:   SqliteDialect{},
			schema:    `sche"ma`,
			table:     `security"rows`,
			indexName: `idx"name`,
			want:      `DROP INDEX "idx""name";`,
		},
		{
			name:      "postgres",
			dialect:   PostgresDialect{},
			schema:    `sche"ma`,
			table:     `security"rows`,
			indexName: `idx"name`,
			indexType: "btree",
			want:      `DROP INDEX "idx""name";`,
		},
		{
			name:      "mysql",
			dialect:   MySQLDialect{Engine: "InnoDB", Encoding: "UTF8"},
			schema:    "sche`ma",
			table:     "security`rows",
			indexName: "idx`name",
			indexType: "Btree",
			want:      "DROP INDEX `idx``name` on `sche``ma`.`security``rows`;",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dbmap := newIdentifierCaptureDbMap(t, tc.dialect)
			table := dbmap.AddTableWithNameAndSchema(indexedRow{}, tc.schema, tc.table)
			table.SetKeys(false, "ID")
			table.AddIndex(tc.indexName, tc.indexType, []string{"ID"})

			err := table.DropIndex(context.Background(), tc.indexName)
			if err != nil {
				t.Fatal(err)
			}

			requireIdentifierCapturedQuery(t, tc.want)
		})
	}
}
