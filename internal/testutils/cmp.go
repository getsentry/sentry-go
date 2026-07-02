package testutils

import (
	"sort"
	"strings"

	"github.com/google/go-cmp/cmp"
)

// EquateKeyValueStrings compares strings after normalizing key-value pair order.
// It handles query strings ("a=1&b=2"), cookie strings ("a=1; b=2"), and
// strings containing a URL query component such as span descriptions.
func EquateKeyValueStrings() cmp.Option {
	return cmp.Comparer(func(x, y string) bool {
		return normalizeComparableString(x) == normalizeComparableString(y)
	})
}

func normalizeComparableString(s string) string {
	return normalizeURLQuery(normalizeKeyValueString(normalizeKeyValueString(s, "&"), ";"))
}

func normalizeURLQuery(s string) string {
	beforeQuery, rest, ok := strings.Cut(s, "?")
	if !ok {
		return s
	}
	query, fragment, hasFragment := strings.Cut(rest, "#")
	normalized := beforeQuery + "?" + normalizeKeyValueString(query, "&")
	if hasFragment {
		normalized += "#" + fragment
	}
	return normalized
}

func normalizeKeyValueString(s, separator string) string {
	parts := strings.Split(s, separator)
	if len(parts) < 2 {
		return s
	}

	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return s
		}
		if !strings.Contains(part, "=") {
			return s
		}
		trimmed = append(trimmed, part)
	}
	sort.Strings(trimmed)
	return strings.Join(trimmed, separator)
}
