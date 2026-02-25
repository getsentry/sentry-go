package sentrysql_test

import (
	"database/sql"
	"fmt"
	"net"

	"github.com/getsentry/sentry-go/sentrysql"
	sqlite "github.com/glebarez/go-sqlite"
	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

func ExampleNewSentrySQL() {
	sql.Register("sentrysql-sqlite", sentrysql.NewSentrySQL(
		&sqlite.Driver{},
		sentrysql.WithDatabaseName(":memory:"),
		sentrysql.WithDatabaseSystem(sentrysql.DatabaseSystem("sqlite")),
	))

	db, err := sql.Open("sentrysql-sqlite", ":memory:")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INT)")
	if err != nil {
		panic(err)
	}

	_, err = db.Exec("INSERT INTO test (id) VALUES (1)")
	if err != nil {
		panic(err)
	}

	rows, err := db.Query("SELECT * FROM test")
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		err = rows.Scan(&id)
		if err != nil {
			panic(err)
		}

		fmt.Println(id)
	}
}

func ExampleNewSentrySQLConnector_postgres() {
	// Create a new PostgreSQL connector that utilizes the `github.com/lib/pq` package.
	pqConnector, err := pq.NewConnector("postgres://user:password@localhost:5432/db")
	if err != nil {
		fmt.Println("creating postgres connector:", err.Error())
		return
	}

	// `db` here is an instance of *sql.DB.
	db := sql.OpenDB(sentrysql.NewSentrySQLConnector(
		pqConnector,
		sentrysql.WithDatabaseName("db"),
		sentrysql.WithDatabaseSystem(sentrysql.PostgreSQL),
		sentrysql.WithServerAddress("localhost", "5432"),
	))
	defer func() {
		err := db.Close()
		if err != nil {
			fmt.Println("closing postgres connection:", err.Error())
		}
	}()

	// Use the db connection as usual.
}

func ExampleNewSentrySQLConnector_mysql() {
	// Create a new MySQL connector that utilizes the `github.com/go-sql-driver/mysql` package.
	config, err := mysql.ParseDSN("user:password@tcp(localhost:3306)/test?parseTime=true")
	if err != nil {
		fmt.Println("parsing mysql dsn:", err.Error())
		return
	}

	mysqlHost, mysqlPort, _ := net.SplitHostPort(config.Addr)

	connector, err := mysql.NewConnector(config)
	if err != nil {
		fmt.Println("creating mysql connector:", err.Error())
		return
	}

	// `db` here is an instance of *sql.DB.
	db := sql.OpenDB(sentrysql.NewSentrySQLConnector(
		connector,
		sentrysql.WithDatabaseName(config.DBName),
		sentrysql.WithDatabaseSystem(sentrysql.MySQL),
		sentrysql.WithServerAddress(mysqlHost, mysqlPort),
	))
	defer func() {
		err := db.Close()
		if err != nil {
			fmt.Println("closing mysql connection:", err.Error())
		}
	}()

	// Use the db connection as usual.
}
