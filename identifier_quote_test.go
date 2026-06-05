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

func TestQuotedTableNameCannotRewriteUpdateTarget(t *testing.T) {
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

func TestCreateIndexQuotesIdentifierMetadata(t *testing.T) {
	type indexedRow struct {
		ID int64 `db:"ID"`
	}

	dbmap := newIdentifierCaptureDbMap(t, PostgresDialect{})
	table := dbmap.AddTableWithNameAndSchema(indexedRow{}, `sche"ma`, `security"rows`)
	table.SetKeys(false, "ID")
	table.AddIndex(`idx"name`, "btree", []string{"ID"})

	err := dbmap.CreateIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	execs := identifierCapturedExecs()
	if len(execs) != 1 {
		t.Fatalf("expected one captured exec, got %d: %+v", len(execs), execs)
	}

	want := `create index "idx""name" on "sche""ma"."security""rows" using btree ("ID");`
	if execs[0].query != want {
		t.Fatalf("generated %q, want %q", execs[0].query, want)
	}
}

func TestDropIndexQuotesIdentifierMetadata(t *testing.T) {
	type indexedRow struct {
		ID int64 `db:"ID"`
	}

	dbmap := newIdentifierCaptureDbMap(t, MySQLDialect{Engine: "InnoDB", Encoding: "UTF8"})
	table := dbmap.AddTableWithNameAndSchema(indexedRow{}, "sche`ma", "security`rows")
	table.SetKeys(false, "ID")
	table.AddIndex("idx`name", "Btree", []string{"ID"})

	err := table.DropIndex(context.Background(), "idx`name")
	if err != nil {
		t.Fatal(err)
	}

	execs := identifierCapturedExecs()
	if len(execs) != 1 {
		t.Fatalf("expected one captured exec, got %d: %+v", len(execs), execs)
	}

	want := "DROP INDEX `idx``name` on `sche``ma`.`security``rows`;"
	if execs[0].query != want {
		t.Fatalf("generated %q, want %q", execs[0].query, want)
	}
}
