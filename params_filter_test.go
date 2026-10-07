package gorm_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// stubConnector answers every statement with no rows, so statements run without a database
type stubConnector struct{}

func (connector stubConnector) Connect(context.Context) (driver.Conn, error) { return stubConn{}, nil }
func (connector stubConnector) Driver() driver.Driver                        { return connector }
func (connector stubConnector) Open(string) (driver.Conn, error)             { return stubConn{}, nil }

type stubConn struct{}

func (conn stubConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not supported") }
func (conn stubConn) Close() error                        { return nil }
func (conn stubConn) Begin() (driver.Tx, error)           { return nil, errors.New("not supported") }

func (conn stubConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}

func (conn stubConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return stubRows{}, nil
}

type stubRows struct{}

func (rows stubRows) Columns() []string         { return nil }
func (rows stubRows) Close() error              { return nil }
func (rows stubRows) Next([]driver.Value) error { return io.EOF }

type stubDialector struct{}

func (dialector stubDialector) Name() string { return "stub" }

func (dialector stubDialector) Initialize(db *gorm.DB) error {
	callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})
	db.ConnPool = sql.OpenDB(stubConnector{})
	return nil
}

func (dialector stubDialector) Migrator(*gorm.DB) gorm.Migrator                { return nil }
func (dialector stubDialector) DataTypeOf(*schema.Field) string                { return "" }
func (dialector stubDialector) DefaultValueOf(*schema.Field) clause.Expression { return nil }

func (dialector stubDialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v interface{}) {
	writer.WriteByte('?')
}

func (dialector stubDialector) QuoteTo(writer clause.Writer, str string) { writer.WriteString(str) }

func (dialector stubDialector) Explain(sql string, vars ...interface{}) string {
	return logger.ExplainSQL(sql, nil, `'`, vars...)
}

// traceLogger keeps the last statement it traced
type traceLogger struct {
	logger.Interface
	SQL string
}

func (l *traceLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	l.SQL, _ = fc()
}

// paramsFilterLogger traces statements without their values
type paramsFilterLogger struct {
	*traceLogger
}

func (l paramsFilterLogger) ParamsFilter(ctx context.Context, sql string, params ...interface{}) (string, []interface{}) {
	return sql, nil
}

func TestParamsFilter(t *testing.T) {
	for _, test := range []struct {
		name     string
		filter   bool
		expected string
	}{
		{name: "logger without a filter", expected: "SELECT 'value'"},
		{name: "logger with a filter", filter: true, expected: "SELECT ?"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tracer := &traceLogger{Interface: logger.Discard}
			var configLogger logger.Interface = tracer
			if test.filter {
				configLogger = paramsFilterLogger{tracer}
			}
			db, err := gorm.Open(stubDialector{}, &gorm.Config{Logger: configLogger})
			if err != nil {
				t.Fatalf("failed to open the stub database: %v", err)
			}

			if err := db.Exec("SELECT ?", "value").Error; err != nil {
				t.Fatalf("failed to exec: %v", err)
			}
			if tracer.SQL != test.expected {
				t.Errorf("exec traced %q, expected %q", tracer.SQL, test.expected)
			}

			tracer.SQL = ""
			var count int
			if err := db.Raw("SELECT ?", "value").Scan(&count).Error; err != nil {
				t.Fatalf("failed to scan: %v", err)
			}
			if tracer.SQL != test.expected {
				t.Errorf("scan traced %q, expected %q", tracer.SQL, test.expected)
			}
		})
	}
}
