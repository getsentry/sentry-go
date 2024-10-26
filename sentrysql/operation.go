package sentrysql

import "strings"

var knownDatabaseOperations = map[string]struct{}{
	"SELECT": struct{}{},
	"INSERT": struct{}{},
	"DELETE": struct{}{},
	"UPDATE": struct{}{},
}

func parseDatabaseOperation(query string) string {
	// The operation is the first word of the query.
	operation := query
	if i := strings.Index(query, " "); i >= 0 {
		operation = strings.ToUpper(query[:i])
	}

	// Only returns known words.
	if _, ok := knownDatabaseOperations[operation]; !ok {
		return ""
	}

	return operation
}
