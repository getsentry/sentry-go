package sentry

import (
	"maps"
	"reflect"
	"slices"
)

// deepCopyValue deep-copies common mutable containers. It falls back to the
// original value when copying is not applicable.
func deepCopyValue(v any) any {
	if v == nil {
		return nil
	}

	// Fast paths for common slice/map types to avoid reflect overhead.
	switch t := v.(type) {
	case []byte:
		return slices.Clone(t)
	case []string:
		return slices.Clone(t)
	case []int:
		return slices.Clone(t)
	case []int64:
		return slices.Clone(t)
	case []float64:
		return slices.Clone(t)
	case []bool:
		return slices.Clone(t)
	case map[string]string:
		return maps.Clone(t)
	}

	return deepCopyValueRec(reflect.ValueOf(v), make(map[uintptr]reflect.Value)).Interface()
}

func deepCopyValueRec(v reflect.Value, seen map[uintptr]reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}

	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		return deepCopyValueRec(v.Elem(), seen)
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		ptr := v.Pointer()
		if cloned, ok := seen[ptr]; ok {
			return cloned
		}
		cloned := deepCopyValueRec(v.Elem(), seen)
		newPtr := reflect.New(cloned.Type())
		seen[ptr] = newPtr
		newPtr.Elem().Set(cloned)
		return newPtr
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		ptr := v.Pointer()
		if cloned, ok := seen[ptr]; ok {
			return cloned
		}
		clone := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		seen[ptr] = clone
		for i := 0; i < v.Len(); i++ {
			clone.Index(i).Set(deepCopyValueRec(v.Index(i), seen))
		}
		return clone
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		ptr := v.Pointer()
		if cloned, ok := seen[ptr]; ok {
			return cloned
		}
		clone := reflect.MakeMapWithSize(v.Type(), v.Len())
		seen[ptr] = clone
		for _, key := range v.MapKeys() {
			clone.SetMapIndex(key, deepCopyValueRec(v.MapIndex(key), seen))
		}
		return clone
	case reflect.Array:
		clone := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			clone.Index(i).Set(deepCopyValueRec(v.Index(i), seen))
		}
		return clone
	default:
		return v
	}
}
