package sentrysql

import "strings"

var knownDatabaseOperations = []string{"SELECT", "INSERT", "DELETE", "UPDATE"}

func parseDatabaseOperation(query string) string {
	// The operation is the first word of the query.
	operation := query
	if i := strings.Index(query, " "); i >= 0 {
		operation = strings.ToUpper(query[:i])
	}

	// Only returns known words.
	for _, knownOperation := range knownDatabaseOperations {
		if operation == knownOperation {
			return operation
		}
	}

	return ""
}
