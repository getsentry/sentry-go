package sentrysql_test

import (
	"database/sql"
	"fmt"

	"github.com/getsentry/sentry-go/sentrysql"
	"github.com/lib/pq"
	ramsqldriver "github.com/proullon/ramsql/driver"
)

func ExampleNewSentrySql() {
	sql.Register("sentrysql-ramsql", sentrysql.NewSentrySql(ramsqldriver.NewDriver(), sentrysql.WithDatabaseName("TestDriver"), sentrysql.WithDatabaseSystem("ramsql"), sentrysql.WithServerAddress("127.0.0.1", "3306")))

	db, err := sql.Open("sentrysql-ramsql", "TestDriver")
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

func ExampleNewSentrySqlConnector() {
	pqConnector, err := pq.NewConnector("postgres://user:password@localhost:5432/db")
	if err != nil {
		panic(err)
	}

	db := sql.OpenDB(sentrysql.NewSentrySqlConnector(pqConnector, sentrysql.WithDatabaseName("db"), sentrysql.WithDatabaseSystem("postgres"), sentrysql.WithServerAddress("localhost", "5432")))
	defer db.Close()

	// Continue executing PostgreSQL queries
}
