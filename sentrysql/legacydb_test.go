//nolint:all
package sentrysql_test

// This file is a fork of
// https://cs.opensource.google/go/go/+/refs/tags/go1.7.6:src/database/sql/fakedb_test.go
//
// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Copyright (c) 2009 The Go Authors. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//   - Redistributions of source code must retain the above copyright
//
// notice, this list of conditions and the following disclaimer.
//   - Redistributions in binary form must reproduce the above
//
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//   - Neither the name of Google Inc. nor the names of its
//
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

var _ = log.Printf

// legacyDriver is a fake database that implements Go's driver.Driver
// interface, just for testing.
//
// It speaks a query language that's semantically similar to but
// syntactically different and simpler than SQL.  The syntax is as
// follows:
//
//	WIPE
//	CREATE|<tablename>|<col>=<type>,<col>=<type>,...
//	  where types are: "string", [u]int{8,16,32,64}, "bool"
//	INSERT|<tablename>|col=val,col2=val2,col3=?
//	SELECT|<tablename>|projectcol1,projectcol2|filtercol=?,filtercol2=?
//
// Any of these can be preceded by PANIC|<method>|, to cause the
// named method on fakeStmt to panic.
//
// When opening a fakeDriver's database, it starts empty with no
// tables. All tables and data are stored in memory only.
type legacyDriver struct {
	mu         sync.Mutex // guards 3 following fields
	openCount  int        // conn opens
	closeCount int        // conn closes
	waitCh     chan struct{}
	waitingCh  chan struct{}
	dbs        map[string]*legacyDB
}

type legacyDB struct {
	name string

	mu           sync.Mutex
	legacyTables map[string]*legacyTable
	badConn      bool
}

type legacyTable struct {
	mu         sync.Mutex
	colname    []string
	coltype    []string
	legacyRows []*legacyRow
}

func (t *legacyTable) columnIndex(name string) int {
	for n, nname := range t.colname {
		if name == nname {
			return n
		}
	}
	return -1
}

type legacyRow struct {
	cols []interface{} // must be same size as its legacyTable colname + coltype
}

type legacyConn struct {
	db *legacyDB // where to return ourselves to

	currTx *legacyTx

	// Stats for tests:
	mu          sync.Mutex
	stmtsMade   int
	stmtsClosed int
	numPrepare  int

	// bad connection tests; see isBad()
	bad       bool
	stickyBad bool
}

func (c *legacyConn) incrStat(v *int) {
	c.mu.Lock()
	*v++
	c.mu.Unlock()
}

type legacyTx struct {
	c *legacyConn
}

type legacyStmt struct {
	c *legacyConn
	q string // just for debugging

	cmd         string
	legacyTable string
	panic       string

	closed bool

	colName      []string      // used by CREATE, INSERT, SELECT (selected columns)
	colType      []string      // used by CREATE
	colValue     []interface{} // used by INSERT (mix of strings and "?" for bound params)
	placeholders int           // used by INSERT/SELECT: number of ? params

	whereCol []string // used by SELECT (all placeholders)

	placeholderConverter []driver.ValueConverter // used by INSERT
}

var ldriver driver.Driver = &legacyDriver{}

// hook to simulate connection failures
var legacyHookOpenErr struct {
	sync.Mutex
	fn func() error
}

// Supports dsn forms:
//
//	<dbname>
//	<dbname>;<opts>  (only currently supported option is `badConn`,
//	                  which causes driver.ErrBadConn to be returned on
//	                  every other conn.Begin())
func (d *legacyDriver) Open(dsn string) (driver.Conn, error) {
	legacyHookOpenErr.Lock()
	fn := legacyHookOpenErr.fn
	legacyHookOpenErr.Unlock()
	if fn != nil {
		if err := fn(); err != nil {
			return nil, err
		}
	}
	parts := strings.Split(dsn, ";")
	if len(parts) < 1 {
		return nil, errors.New("fakedb: no database name")
	}
	name := parts[0]

	db := d.getDB(name)

	d.mu.Lock()
	d.openCount++
	d.mu.Unlock()
	conn := &legacyConn{db: db}

	if len(parts) >= 2 && parts[1] == "badConn" {
		conn.bad = true
	}
	if d.waitCh != nil {
		d.waitingCh <- struct{}{}
		<-d.waitCh
		d.waitCh = nil
		d.waitingCh = nil
	}
	return conn, nil
}

func (d *legacyDriver) getDB(name string) *legacyDB {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dbs == nil {
		d.dbs = make(map[string]*legacyDB)
	}
	db, ok := d.dbs[name]
	if !ok {
		db = &legacyDB{name: name}
		d.dbs[name] = db
	}
	return db
}

func (db *legacyDB) wipe() {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.legacyTables = nil
}

func (db *legacyDB) createTable(name string, columnNames, columnTypes []string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.legacyTables == nil {
		db.legacyTables = make(map[string]*legacyTable)
	}
	if _, exist := db.legacyTables[name]; exist {
		return fmt.Errorf("legacyTable %q already exists", name)
	}
	if len(columnNames) != len(columnTypes) {
		return fmt.Errorf("create legacyTable of %q len(names) != len(types): %d vs %d",
			name, len(columnNames), len(columnTypes))
	}
	db.legacyTables[name] = &legacyTable{colname: columnNames, coltype: columnTypes}
	return nil
}

// must be called with db.mu lock held
func (db *legacyDB) legacyTable(legacyTable string) (*legacyTable, bool) {
	if db.legacyTables == nil {
		return nil, false
	}
	t, ok := db.legacyTables[legacyTable]
	return t, ok
}

func (db *legacyDB) columnType(legacyTable, column string) (typ string, ok bool) {
	db.mu.Lock()
	defer db.mu.Unlock()
	t, ok := db.legacyTable(legacyTable)
	if !ok {
		return
	}
	for n, cname := range t.colname {
		if cname == column {
			return t.coltype[n], true
		}
	}
	return "", false
}

func (c *legacyConn) isBad() bool {
	if c.stickyBad {
		return true
	} else if c.bad {
		// alternate between bad conn and not bad conn
		c.db.badConn = !c.db.badConn
		return c.db.badConn
	} else {
		return false
	}
}

func (c *legacyConn) Begin() (driver.Tx, error) {
	if c.isBad() {
		return nil, driver.ErrBadConn
	}
	if c.currTx != nil {
		return nil, errors.New("already in a transaction")
	}
	c.currTx = &legacyTx{c: c}
	return c.currTx, nil
}

var legacyHookPostCloseConn struct {
	sync.Mutex
	fn func(*legacyConn, error)
}

func (c *legacyConn) Close() (err error) {
	drv := ldriver.(*legacyDriver)
	defer func() {
		if err != nil && testStrictClose != nil {
			testStrictClose.Errorf("failed to close a test legacyConn: %v", err)
		}
		legacyHookPostCloseConn.Lock()
		fn := legacyHookPostCloseConn.fn
		legacyHookPostCloseConn.Unlock()
		if fn != nil {
			fn(c, err)
		}
		if err == nil {
			drv.mu.Lock()
			drv.closeCount++
			drv.mu.Unlock()
		}
	}()
	if c.currTx != nil {
		return errors.New("can't close legacyConn; in a Transaction")
	}
	if c.db == nil {
		return errors.New("can't close legacyConn; already closed")
	}
	if c.stmtsMade > c.stmtsClosed {
		return errors.New("can't close; dangling statement(s)")
	}
	c.db = nil
	return nil
}

func legacyCheckSubsetTypes(args []driver.Value) error {
	for n, arg := range args {
		switch arg.(type) {
		case int64, float64, bool, nil, []byte, string, time.Time:
		default:
			return fmt.Errorf("fakedb_test: invalid argument #%d: %v, type %T", n+1, arg, arg)
		}
	}
	return nil
}

func (c *legacyConn) Exec(query string, args []driver.Value) (driver.Result, error) {
	// This is an optional interface, but it's implemented here
	// just to check that all the args are of the proper types.
	// ErrSkip is returned so the caller acts as if we didn't
	// implement this at all.
	err := legacyCheckSubsetTypes(args)
	if err != nil {
		return nil, err
	}
	return nil, driver.ErrSkip
}

func (c *legacyConn) Query(query string, args []driver.Value) (driver.Rows, error) {
	// This is an optional interface, but it's implemented here
	// just to check that all the args are of the proper types.
	// ErrSkip is returned so the caller acts as if we didn't
	// implement this at all.
	err := legacyCheckSubsetTypes(args)
	if err != nil {
		return nil, err
	}
	return nil, driver.ErrSkip
}

// parts are legacyTable|selectCol1,selectCol2|whereCol=?,whereCol2=?
// (note that where columns must always contain ? marks,
//
//	just a limitation for fakedb)
func (c *legacyConn) prepareSelect(stmt *legacyStmt, parts []string) (driver.Stmt, error) {
	if len(parts) != 3 {
		stmt.Close()
		return nil, errf("invalid SELECT syntax with %d parts; want 3", len(parts))
	}
	stmt.legacyTable = parts[0]
	stmt.colName = strings.Split(parts[1], ",")
	for n, colspec := range strings.Split(parts[2], ",") {
		if colspec == "" {
			continue
		}
		nameVal := strings.Split(colspec, "=")
		if len(nameVal) != 2 {
			stmt.Close()
			return nil, errf("SELECT on legacyTable %q has invalid column spec of %q (index %d)", stmt.legacyTable, colspec, n)
		}
		column, value := nameVal[0], nameVal[1]
		_, ok := c.db.columnType(stmt.legacyTable, column)
		if !ok {
			stmt.Close()
			return nil, errf("SELECT on legacyTable %q references non-existent column %q", stmt.legacyTable, column)
		}
		if value != "?" {
			stmt.Close()
			return nil, errf("SELECT on legacyTable %q has pre-bound value for where column %q; need a question mark",
				stmt.legacyTable, column)
		}
		stmt.whereCol = append(stmt.whereCol, column)
		stmt.placeholders++
	}
	return stmt, nil
}

// parts are legacyTable|col=type,col2=type2
func (c *legacyConn) prepareCreate(stmt *legacyStmt, parts []string) (driver.Stmt, error) {
	if len(parts) != 2 {
		stmt.Close()
		return nil, errf("invalid CREATE syntax with %d parts; want 2", len(parts))
	}
	stmt.legacyTable = parts[0]
	for n, colspec := range strings.Split(parts[1], ",") {
		nameType := strings.Split(colspec, "=")
		if len(nameType) != 2 {
			stmt.Close()
			return nil, errf("CREATE legacyTable %q has invalid column spec of %q (index %d)", stmt.legacyTable, colspec, n)
		}
		stmt.colName = append(stmt.colName, nameType[0])
		stmt.colType = append(stmt.colType, nameType[1])
	}
	return stmt, nil
}

// parts are legacyTable|col=?,col2=val
func (c *legacyConn) prepareInsert(stmt *legacyStmt, parts []string) (driver.Stmt, error) {
	if len(parts) != 2 {
		stmt.Close()
		return nil, errf("invalid INSERT syntax with %d parts; want 2", len(parts))
	}
	stmt.legacyTable = parts[0]
	for n, colspec := range strings.Split(parts[1], ",") {
		nameVal := strings.Split(colspec, "=")
		if len(nameVal) != 2 {
			stmt.Close()
			return nil, errf("INSERT legacyTable %q has invalid column spec of %q (index %d)", stmt.legacyTable, colspec, n)
		}
		column, value := nameVal[0], nameVal[1]
		ctype, ok := c.db.columnType(stmt.legacyTable, column)
		if !ok {
			stmt.Close()
			return nil, errf("INSERT legacyTable %q references non-existent column %q", stmt.legacyTable, column)
		}
		stmt.colName = append(stmt.colName, column)

		if value != "?" {
			var subsetVal interface{}
			// Convert to driver subset type
			switch ctype {
			case "string":
				subsetVal = []byte(value)
			case "blob":
				subsetVal = []byte(value)
			case "int32":
				i, err := strconv.Atoi(value)
				if err != nil {
					stmt.Close()
					return nil, errf("invalid conversion to int32 from %q", value)
				}
				subsetVal = int64(i) // int64 is a subset type, but not int32
			default:
				stmt.Close()
				return nil, errf("unsupported conversion for pre-bound parameter %q to type %q", value, ctype)
			}
			stmt.colValue = append(stmt.colValue, subsetVal)
		} else {
			stmt.placeholders++
			stmt.placeholderConverter = append(stmt.placeholderConverter, converterForType(ctype))
			stmt.colValue = append(stmt.colValue, "?")
		}
	}
	return stmt, nil
}

// hook to simulate broken connections
var legacyHookPrepareBadConn func() bool

func (c *legacyConn) Prepare(query string) (driver.Stmt, error) {
	c.numPrepare++
	if c.db == nil {
		panic("nil c.db; conn = " + fmt.Sprintf("%#v", c))
	}

	if c.stickyBad || (legacyHookPrepareBadConn != nil && legacyHookPrepareBadConn()) {
		return nil, driver.ErrBadConn
	}

	parts := strings.Split(query, "|")
	if len(parts) < 1 {
		return nil, errf("empty query")
	}
	stmt := &legacyStmt{q: query, c: c}
	if len(parts) >= 3 && parts[0] == "PANIC" {
		stmt.panic = parts[1]
		parts = parts[2:]
	}
	cmd := parts[0]
	stmt.cmd = cmd
	parts = parts[1:]

	c.incrStat(&c.stmtsMade)
	switch cmd {
	case "WIPE":
		// Nothing
	case "SELECT":
		return c.prepareSelect(stmt, parts)
	case "CREATE":
		return c.prepareCreate(stmt, parts)
	case "INSERT":
		return c.prepareInsert(stmt, parts)
	case "NOSERT":
		// Do all the prep-work like for an INSERT but don't actually insert the legacyRow.
		// Used for some of the concurrent tests.
		return c.prepareInsert(stmt, parts)
	default:
		stmt.Close()
		return nil, errf("unsupported command type %q", cmd)
	}
	return stmt, nil
}

func (s *legacyStmt) ColumnConverter(idx int) driver.ValueConverter {
	if s.panic == "ColumnConverter" {
		panic(s.panic)
	}
	if len(s.placeholderConverter) == 0 {
		return driver.DefaultParameterConverter
	}
	return s.placeholderConverter[idx]
}

func (s *legacyStmt) Close() error {
	if s.panic == "Close" {
		panic(s.panic)
	}
	if s.c == nil {
		panic("nil conn in legacyStmt.Close")
	}
	if s.c.db == nil {
		panic("in legacyStmt.Close, conn's db is nil (already closed)")
	}
	if !s.closed {
		s.c.incrStat(&s.c.stmtsClosed)
		s.closed = true
	}
	return nil
}

// hook to simulate broken connections
var legacyHookExecBadConn func() bool

func (s *legacyStmt) Exec(args []driver.Value) (driver.Result, error) {
	if s.panic == "Exec" {
		panic(s.panic)
	}
	if s.closed {
		return nil, errClosed
	}

	if s.c.stickyBad || (legacyHookExecBadConn != nil && legacyHookExecBadConn()) {
		return nil, driver.ErrBadConn
	}

	err := legacyCheckSubsetTypes(args)
	if err != nil {
		return nil, err
	}

	db := s.c.db
	switch s.cmd {
	case "WIPE":
		db.wipe()
		return driver.ResultNoRows, nil
	case "CREATE":
		if err := db.createTable(s.legacyTable, s.colName, s.colType); err != nil {
			return nil, err
		}
		return driver.ResultNoRows, nil
	case "INSERT":
		return s.execInsert(args, true)
	case "NOSERT":
		// Do all the prep-work like for an INSERT but don't actually insert the legacyRow.
		// Used for some of the concurrent tests.
		return s.execInsert(args, false)
	}
	fmt.Printf("EXEC statement, cmd=%q: %#v\n", s.cmd, s)
	return nil, fmt.Errorf("unimplemented statement Exec command type of %q", s.cmd)
}

// When doInsert is true, add the legacyRow to the legacyTable.
// When doInsert is false do prep-work and error checking, but don't
// actually add the legacyRow to the legacyTable.
func (s *legacyStmt) execInsert(args []driver.Value, doInsert bool) (driver.Result, error) {
	db := s.c.db
	if len(args) != s.placeholders {
		panic("error in pkg db; should only get here if size is correct")
	}
	db.mu.Lock()
	t, ok := db.legacyTable(s.legacyTable)
	db.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("fakedb: legacyTable %q doesn't exist", s.legacyTable)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	var cols []interface{}
	if doInsert {
		cols = make([]interface{}, len(t.colname))
	}
	argPos := 0
	for n, colname := range s.colName {
		colidx := t.columnIndex(colname)
		if colidx == -1 {
			return nil, fmt.Errorf("fakedb: column %q doesn't exist or dropped since prepared statement was created", colname)
		}
		var val interface{}
		if strvalue, ok := s.colValue[n].(string); ok && strvalue == "?" {
			val = args[argPos]
			argPos++
		} else {
			val = s.colValue[n]
		}
		if doInsert {
			cols[colidx] = val
		}
	}

	if doInsert {
		t.legacyRows = append(t.legacyRows, &legacyRow{cols: cols})
	}
	return driver.RowsAffected(1), nil
}

// hook to simulate broken connections
var legacyHookQueryBadConn func() bool

func (s *legacyStmt) Query(args []driver.Value) (driver.Rows, error) {
	if s.panic == "Query" {
		panic(s.panic)
	}
	if s.closed {
		return nil, errClosed
	}

	if s.c.stickyBad || (legacyHookQueryBadConn != nil && legacyHookQueryBadConn()) {
		return nil, driver.ErrBadConn
	}

	err := legacyCheckSubsetTypes(args)
	if err != nil {
		return nil, err
	}

	db := s.c.db
	if len(args) != s.placeholders {
		panic("error in pkg db; should only get here if size is correct")
	}

	db.mu.Lock()
	t, ok := db.legacyTable(s.legacyTable)
	db.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("fakedb: legacyTable %q doesn't exist", s.legacyTable)
	}

	if s.legacyTable == "magicquery" {
		if len(s.whereCol) == 2 && s.whereCol[0] == "op" && s.whereCol[1] == "millis" {
			if args[0] == "sleep" {
				time.Sleep(time.Duration(args[1].(int64)) * time.Millisecond)
			}
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	colIdx := make(map[string]int) // select column name -> column index in legacyTable
	for _, name := range s.colName {
		idx := t.columnIndex(name)
		if idx == -1 {
			return nil, fmt.Errorf("fakedb: unknown column name %q", name)
		}
		colIdx[name] = idx
	}

	mlegacyRows := []*legacyRow{}
legacyRows:
	for _, tlegacyRow := range t.legacyRows {
		// Process the where clause, skipping non-match legacyRows. This is lazy
		// and just uses fmt.Sprintf("%v") to test equality. Good enough
		// for test code.
		for widx, wcol := range s.whereCol {
			idx := t.columnIndex(wcol)
			if idx == -1 {
				return nil, fmt.Errorf("db: invalid where clause column %q", wcol)
			}
			tcol := tlegacyRow.cols[idx]
			if bs, ok := tcol.([]byte); ok {
				// lazy hack to avoid sprintf %v on a []byte
				tcol = string(bs)
			}
			if fmt.Sprintf("%v", tcol) != fmt.Sprintf("%v", args[widx]) {
				continue legacyRows
			}
		}
		mlegacyRow := &legacyRow{cols: make([]interface{}, len(s.colName))}
		for seli, name := range s.colName {
			mlegacyRow.cols[seli] = tlegacyRow.cols[colIdx[name]]
		}
		mlegacyRows = append(mlegacyRows, mlegacyRow)
	}

	cursor := &legacyRowsCursor{
		pos:        -1,
		legacyRows: mlegacyRows,
		cols:       s.colName,
		errPos:     -1,
	}
	return cursor, nil
}

func (s *legacyStmt) NumInput() int {
	if s.panic == "NumInput" {
		panic(s.panic)
	}
	return s.placeholders
}

// hook to simulate broken connections
var legacyHookCommitBadConn func() bool

func (tx *legacyTx) Commit() error {
	tx.c.currTx = nil
	if legacyHookCommitBadConn != nil && legacyHookCommitBadConn() {
		return driver.ErrBadConn
	}
	return nil
}

// hook to simulate broken connections
var legacyHookRollbackBadConn func() bool

func (tx *legacyTx) Rollback() error {
	tx.c.currTx = nil
	if legacyHookRollbackBadConn != nil && legacyHookRollbackBadConn() {
		return driver.ErrBadConn
	}
	return nil
}

type legacyRowsCursor struct {
	cols       []string
	pos        int
	legacyRows []*legacyRow
	closed     bool

	// errPos and err are for making Next return early with error.
	errPos int
	err    error

	// a clone of slices to give out to clients, indexed by the
	// the original slice's first byte address.  we clone them
	// just so we're able to corrupt them on close.
	bytesClone map[*byte][]byte
}

func (rc *legacyRowsCursor) Close() error {
	if !rc.closed {
		for _, bs := range rc.bytesClone {
			bs[0] = 255 // first byte corrupted
		}
	}
	rc.closed = true
	return nil
}

func (rc *legacyRowsCursor) Columns() []string {
	return rc.cols
}

var legacyRowsCursorNextHook func(dest []driver.Value) error

func (rc *legacyRowsCursor) Next(dest []driver.Value) error {
	if legacyRowsCursorNextHook != nil {
		return legacyRowsCursorNextHook(dest)
	}

	if rc.closed {
		return errors.New("fakedb: cursor is closed")
	}
	rc.pos++
	if rc.pos == rc.errPos {
		return rc.err
	}
	if rc.pos >= len(rc.legacyRows) {
		return io.EOF // per interface spec
	}
	for i, v := range rc.legacyRows[rc.pos].cols {
		// TODO(bradfitz): convert to subset types? naah, I
		// think the subset types should only be input to
		// driver, but the sql package should be able to handle
		// a wider range of types coming out of drivers. all
		// for ease of drivers, and to prevent drivers from
		// messing up conversions or doing them differently.
		dest[i] = v

		if bs, ok := v.([]byte); ok {
			if rc.bytesClone == nil {
				rc.bytesClone = make(map[*byte][]byte)
			}
			clone, ok := rc.bytesClone[&bs[0]]
			if !ok {
				clone = make([]byte, len(bs))
				copy(clone, bs)
				rc.bytesClone[&bs[0]] = clone
			}
			dest[i] = clone
		}
	}
	return nil
}

// legacyDriverString is like driver.String, but indirects pointers like
// DefaultValueConverter.
//
// This could be surprising behavior to retroactively apply to
// driver.String now that Go1 is out, but this is convenient for
// our TestPointerParamsAndScans.
type legacyDriverString struct{}

func (legacyDriverString) ConvertValue(v interface{}) (driver.Value, error) {
	switch c := v.(type) {
	case string, []byte:
		return v, nil
	case *string:
		if c == nil {
			return nil, nil
		}
		return *c, nil
	}
	return fmt.Sprintf("%v", v), nil
}
