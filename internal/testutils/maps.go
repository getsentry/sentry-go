package testutils

// MergeMaps merges multiple maps into a single map.
// If there are duplicate keys, the value from the last map takes precedence.
//
// CC BY-SA 4.0 Oliver (https://stackoverflow.com/a/74750675/3153224)
func MergeMaps[M ~map[K]V, K comparable, V any](src ...M) M {
	merged := make(M)
	for _, m := range src {
		for k, v := range m {
			merged[k] = v
		}
	}
	return merged
}
