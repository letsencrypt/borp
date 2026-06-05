package borp

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"sync"
	"testing"
)

type updateColumnsSecurityRow struct {
	ID         int64
	PublicNote string
	Admin      bool
	Balance    int64
}

type capturedExec struct {
	query string
	args  []driver.NamedValue
}

var captureDriverState = struct {
	sync.Mutex
	registered sync.Once
	execs      []capturedExec
}{}

type captureDriver struct{}

func (captureDriver) Open(string) (driver.Conn, error) {
	return captureConn{}, nil
}

type captureConn struct{}

func (captureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("capture driver does not prepare statements")
}

func (captureConn) Close() error {
	return nil
}

func (captureConn) Begin() (driver.Tx, error) {
	return nil, errors.New("capture driver does not begin transactions")
}

func (captureConn) ExecContext(
	_ context.Context,
	query string,
	args []driver.NamedValue,
) (driver.Result, error) {
	copied := append([]driver.NamedValue(nil), args...)
	captureDriverState.Lock()
	captureDriverState.execs = append(captureDriverState.execs, capturedExec{
		query: query,
		args:  copied,
	})
	captureDriverState.Unlock()
	return driver.RowsAffected(1), nil
}

func newCaptureDbMap(t *testing.T) *DbMap {
	t.Helper()
	captureDriverState.registered.Do(func() {
		sql.Register("borp_capture", captureDriver{})
	})
	captureDriverState.Lock()
	captureDriverState.execs = nil
	captureDriverState.Unlock()

	db, err := sql.Open("borp_capture", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		err := db.Close()
		if err != nil {
			t.Error(err)
		}
	})

	dbmap := &DbMap{Db: db, Dialect: SqliteDialect{}}
	dbmap.AddTableWithName(updateColumnsSecurityRow{}, "security_rows").SetKeys(true, "ID")
	return dbmap
}

func captureExecs() []capturedExec {
	captureDriverState.Lock()
	defer captureDriverState.Unlock()
	return append([]capturedExec(nil), captureDriverState.execs...)
}

func capturedExecValues(exec capturedExec) []interface{} {
	values := make([]interface{}, len(exec.args))
	for i, arg := range exec.args {
		values[i] = arg.Value
	}
	return values
}

func requireCapturedExec(
	t *testing.T,
	got capturedExec,
	wantQuery string,
	wantValues []interface{},
) {
	t.Helper()

	if got.query != wantQuery {
		t.Fatalf("generated %q, want %q; args: %+v", got.query, wantQuery, got.args)
	}

	values := capturedExecValues(got)
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("bound values = %#v, want %#v; query: %s", values, wantValues, got.query)
	}
}

func TestUpdateColumnsDoesNotReusePriorFullUpdatePlan(t *testing.T) {
	dbmap := newCaptureDbMap(t)
	ctx := context.Background()

	_, err := dbmap.Update(ctx, &updateColumnsSecurityRow{
		ID:         7,
		PublicNote: "initial",
		Admin:      false,
		Balance:    10,
	})
	if err != nil {
		t.Fatal(err)
	}

	onlyPublicNote := func(col *ColumnMap) bool {
		return col.ColumnName == "PublicNote"
	}
	_, err = dbmap.UpdateColumns(ctx, onlyPublicNote, &updateColumnsSecurityRow{
		ID:         7,
		PublicNote: "attacker controlled note",
		Admin:      true,
		Balance:    999,
	})
	if err != nil {
		t.Fatal(err)
	}

	execs := captureExecs()
	if len(execs) != 2 {
		t.Fatalf("expected two captured execs, got %d: %+v", len(execs), execs)
	}

	got := execs[1]
	wantQuery := `update "security_rows" set "PublicNote"=? where "ID"=?;`
	requireCapturedExec(t, got, wantQuery, []interface{}{
		"attacker controlled note",
		int64(7),
	})
}

func TestUpdateColumnsDoesNotReusePriorFilteredUpdatePlan(t *testing.T) {
	dbmap := newCaptureDbMap(t)
	ctx := context.Background()

	adminAndBalance := func(col *ColumnMap) bool {
		return col.ColumnName == "Admin" || col.ColumnName == "Balance"
	}
	_, err := dbmap.UpdateColumns(ctx, adminAndBalance, &updateColumnsSecurityRow{
		ID:         7,
		PublicNote: "not updated",
		Admin:      true,
		Balance:    999,
	})
	if err != nil {
		t.Fatal(err)
	}

	onlyPublicNote := func(col *ColumnMap) bool {
		return col.ColumnName == "PublicNote"
	}
	_, err = dbmap.UpdateColumns(ctx, onlyPublicNote, &updateColumnsSecurityRow{
		ID:         7,
		PublicNote: "second filtered update",
		Admin:      false,
		Balance:    10,
	})
	if err != nil {
		t.Fatal(err)
	}

	execs := captureExecs()
	if len(execs) != 2 {
		t.Fatalf("expected two captured execs, got %d: %+v", len(execs), execs)
	}

	requireCapturedExec(
		t,
		execs[0],
		`update "security_rows" set "Admin"=?, "Balance"=? where "ID"=?;`,
		[]interface{}{true, int64(999), int64(7)},
	)
	requireCapturedExec(
		t,
		execs[1],
		`update "security_rows" set "PublicNote"=? where "ID"=?;`,
		[]interface{}{"second filtered update", int64(7)},
	)
}
