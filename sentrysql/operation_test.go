package sentrysql

import "testing"

func TestParseDatabaseOperation(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "SELECT",
			query: "SELECT * FROM users",
			want:  "SELECT",
		},
		{
			name:  "INSERT",
			query: "INSERT INTO users (id, name) VALUES (1, 'John')",
			want:  "INSERT",
		},
		{
			name:  "DELETE",
			query: "DELETE FROM users WHERE id = 1",
			want:  "DELETE",
		},
		{
			name:  "UPDATE",
			query: "UPDATE users SET name = 'John' WHERE id = 1",
			want:  "UPDATE",
		},
		{
			name:  "findById",
			query: "findById",
			want:  "",
		},
		{
			name:  "Empty",
			query: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseDatabaseOperation(tt.query); got != tt.want {
				t.Errorf("parseDatabaseOperation() = %v, want %v", got, tt.want)
			}
		})
	}
}
