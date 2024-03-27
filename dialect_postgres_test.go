// Copyright 2012 James Cooper. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build !integration
// +build !integration

package borp_test

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/letsencrypt/borp"
	"github.com/poy/onpar"
	"github.com/poy/onpar/expect"
	"github.com/poy/onpar/matchers"
)

type postgresTestContext struct {
	expect  expect.Expectation
	dialect borp.PostgresDialect
}

func TestPostgresDialect(t *testing.T) {
	o := onpar.BeforeEach(onpar.New(t), func(t *testing.T) postgresTestContext {
		return postgresTestContext{
			expect.New(t),
			borp.PostgresDialect{
				LowercaseFields: false,
			},
		}
	})

	defer o.Run()

	o.Group("ToSqlType", func() {
		tests := []struct {
			name     string
			value    interface{}
			maxSize  int
			autoIncr bool
			expected string
		}{
			{"bool", true, 0, false, "boolean"},
			{"int8", int8(1), 0, false, "integer"},
			{"uint8", uint8(1), 0, false, "integer"},
			{"int16", int16(1), 0, false, "integer"},
			{"uint16", uint16(1), 0, false, "integer"},
			{"int32", int32(1), 0, false, "integer"},
			{"int (treated as int32)", int(1), 0, false, "integer"},
			{"uint32", uint32(1), 0, false, "integer"},
			{"uint (treated as uint32)", uint(1), 0, false, "integer"},
			{"int64", int64(1), 0, false, "bigint"},
			{"uint64", uint64(1), 0, false, "bigint"},
			{"float32", float32(1), 0, false, "real"},
			{"float64", float64(1), 0, false, "double precision"},
			{"[]uint8", []uint8{1}, 0, false, "bytea"},
			{"NullInt64", sql.NullInt64{}, 0, false, "bigint"},
			{"NullFloat64", sql.NullFloat64{}, 0, false, "double precision"},
			{"NullBool", sql.NullBool{}, 0, false, "boolean"},
			{"Time", time.Time{}, 0, false, "timestamp with time zone"},
			{"default-size string", "", 0, false, "text"},
			{"sized string", "", 50, false, "varchar(50)"},
			{"large string", "", 1024, false, "varchar(1024)"},
		}
		for _, t := range tests {
			o.Spec(t.name, func(tcx postgresTestContext) {
				typ := reflect.TypeOf(t.value)
				sqlType := tcx.dialect.ToSqlType(typ, t.maxSize, t.autoIncr)
				tcx.expect(sqlType).To(matchers.Equal(t.expected))
			})
		}
	})

	o.Spec("AutoIncrStr", func(tcx postgresTestContext) {
		tcx.expect(tcx.dialect.AutoIncrStr()).To(matchers.Equal(""))
	})

	o.Spec("AutoIncrBindValue", func(tcx postgresTestContext) {
		tcx.expect(tcx.dialect.AutoIncrBindValue()).To(matchers.Equal("default"))
	})

	o.Spec("AutoIncrInsertSuffix", func(tcx postgresTestContext) {
		cm := borp.ColumnMap{
			ColumnName: "foo",
		}
		tcx.expect(tcx.dialect.AutoIncrInsertSuffix(&cm)).To(matchers.Equal(` returning "foo"`))
	})

	o.Spec("CreateTableSuffix", func(tcx postgresTestContext) {
		tcx.expect(tcx.dialect.CreateTableSuffix()).To(matchers.Equal(""))
	})

	o.Spec("CreateIndexSuffix", func(tcx postgresTestContext) {
		tcx.expect(tcx.dialect.CreateIndexSuffix()).To(matchers.Equal("using"))
	})

	o.Spec("DropIndexSuffix", func(tcx postgresTestContext) {
		tcx.expect(tcx.dialect.DropIndexSuffix()).To(matchers.Equal(""))
	})

	o.Spec("TruncateClause", func(tcx postgresTestContext) {
		tcx.expect(tcx.dialect.TruncateClause()).To(matchers.Equal("truncate"))
	})

	o.Spec("SleepClause", func(tcx postgresTestContext) {
		tcx.expect(tcx.dialect.SleepClause(1 * time.Second)).To(matchers.Equal("pg_sleep(1.000000)"))
		tcx.expect(tcx.dialect.SleepClause(100 * time.Millisecond)).To(matchers.Equal("pg_sleep(0.100000)"))
	})

	o.Spec("BindVar", func(tcx postgresTestContext) {
		tcx.expect(tcx.dialect.BindVar(0)).To(matchers.Equal("$1"))
		tcx.expect(tcx.dialect.BindVar(4)).To(matchers.Equal("$5"))
	})

	o.Group("QuoteField", func() {
		o.Spec("By default, case is preserved", func(tcx postgresTestContext) {
			tcx.expect(tcx.dialect.QuoteField("Foo")).To(matchers.Equal(`"Foo"`))
			tcx.expect(tcx.dialect.QuoteField("bar")).To(matchers.Equal(`"bar"`))
		})

		o.Group("With LowercaseFields set to true", func() {
			o := onpar.BeforeEach(o, func(tcx postgresTestContext) postgresTestContext {
				tcx.dialect.LowercaseFields = true
				return postgresTestContext{tcx.expect, tcx.dialect}
			})

			o.Spec("fields are lowercased", func(tcx postgresTestContext) {
				tcx.expect(tcx.dialect.QuoteField("Foo")).To(matchers.Equal(`"foo"`))
			})
		})
	})

	o.Group("QuotedTableForQuery", func() {
		o.Spec("using the default schema", func(tcx postgresTestContext) {
			tcx.expect(tcx.dialect.QuotedTableForQuery("", "foo")).To(matchers.Equal(`"foo"`))
		})

		o.Spec("with a supplied schema", func(tcx postgresTestContext) {
			tcx.expect(tcx.dialect.QuotedTableForQuery("foo", "bar")).To(matchers.Equal(`foo."bar"`))
		})
	})

	o.Spec("IfSchemaNotExists", func(tcx postgresTestContext) {
		tcx.expect(tcx.dialect.IfSchemaNotExists("foo", "bar")).To(matchers.Equal("foo if not exists"))
	})

	o.Spec("IfTableExists", func(tcx postgresTestContext) {
		tcx.expect(tcx.dialect.IfTableExists("foo", "bar", "baz")).To(matchers.Equal("foo if exists"))
	})

	o.Spec("IfTableNotExists", func(tcx postgresTestContext) {
		tcx.expect(tcx.dialect.IfTableNotExists("foo", "bar", "baz")).To(matchers.Equal("foo if not exists"))
	})
}
