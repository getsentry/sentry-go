package sentrysql

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestObfuscationDatabaseSystemMappings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		system DatabaseSystem
		input  string
		want   string
	}{
		{
			name:   "mariadb uses mysql lexer mode",
			system: SystemMariaDB,
			input:  `SELECT * FROM t WHERE name = "alice"`,
			want:   "SELECT * FROM t WHERE name = ?",
		},
		{
			name:   "sqlite treats double-quoted tokens as values",
			system: SystemSQLite,
			input:  `SELECT "users"."name" FROM "users" WHERE id = 1`,
			want:   `SELECT ? FROM ? WHERE id = ?`,
		},
		{
			name:   "clickhouse uses generic lexer mode",
			system: SystemClickhouse,
			input:  `SELECT "users"."name" FROM "users" WHERE id = 1`,
			want:   `SELECT "users"."name" FROM "users" WHERE id = ?`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (&config{system: tt.system, obfuscatorDBMS: obfuscatorDBMS(tt.system)}).obfuscateQuery(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
