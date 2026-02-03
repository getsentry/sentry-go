package util

import (
	"maps"
	"reflect"
	"slices"
	"unsafe"
)

type deepCopySeenKey struct {
	kind reflect.Kind
	ptr  uintptr
	len  int
	cap  int
}

func newDeepCopySeenKey(v reflect.Value) deepCopySeenKey {
	key := deepCopySeenKey{kind: v.Kind()}

	switch v.Kind() {
	case reflect.Pointer, reflect.Map:
		key.ptr = v.Pointer()
	case reflect.Slice:
		key.ptr = v.Pointer()
		key.len = v.Len()
		key.cap = v.Cap()
	}

	return key
}

// Deepcopy deep-copies common mutable containers. It falls back to the
// original value when copying is not applicable.
func Deepcopy(v any) any {
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

	return deepCopyValueRec(reflect.ValueOf(v), make(map[deepCopySeenKey]reflect.Value)).Interface()
}

func deepCopyValueRec(v reflect.Value, seen map[deepCopySeenKey]reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}

	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		cloned := deepCopyValueRec(v.Elem(), seen)
		out := reflect.New(v.Type()).Elem()
		out.Set(cloned)
		return out
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		key := newDeepCopySeenKey(v)
		if cloned, ok := seen[key]; ok {
			return cloned
		}
		newPtr := reflect.New(v.Type().Elem())
		seen[key] = newPtr
		newPtr.Elem().Set(deepCopyValueRec(v.Elem(), seen))
		return newPtr
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		key := newDeepCopySeenKey(v)
		if cloned, ok := seen[key]; ok {
			return cloned
		}
		clone := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		seen[key] = clone
		for i := 0; i < v.Len(); i++ {
			clone.Index(i).Set(deepCopyValueRec(v.Index(i), seen))
		}
		return clone
	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		key := newDeepCopySeenKey(v)
		if cloned, ok := seen[key]; ok {
			return cloned
		}
		clone := reflect.MakeMapWithSize(v.Type(), v.Len())
		seen[key] = clone
		for _, key := range v.MapKeys() {
			clone.SetMapIndex(
				deepCopyValueRec(key, seen),
				deepCopyValueRec(v.MapIndex(key), seen),
			)
		}
		return clone
	case reflect.Array:
		clone := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			clone.Index(i).Set(deepCopyValueRec(v.Index(i), seen))
		}
		return clone
	case reflect.Struct:
		// If we receive an interface, we need to make it addressable so we can safely read unexported fields.
		v = makeAddressable(v)
		clone := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			copied := deepCopyValueRec(readableValue(v.Field(i)), seen)
			setStructField(clone.Field(i), copied)
		}
		return clone
	default:
		return v
	}
}

// setStructField writes val into dst, including unexported fields, dst must be addressable.
func setStructField(dst reflect.Value, val reflect.Value) {
	val = readableValue(val)
	if dst.CanSet() {
		dst.Set(val)
		return
	}
	if dst.CanAddr() {
		dst = reflect.NewAt(dst.Type(), unsafe.Pointer(dst.UnsafeAddr())).Elem()
		dst.Set(val)
	}
}

// makeAddressable returns an addressable copy of v if needed.
func makeAddressable(v reflect.Value) reflect.Value {
	if v.CanAddr() {
		return v
	}
	out := reflect.New(v.Type()).Elem()
	out.Set(v)
	return out
}

// readableValue returns a reflect.Value that can be interfaced/assigned even
// for unexported fields by using unsafe when necessary.
func readableValue(v reflect.Value) reflect.Value {
	if v.CanInterface() {
		return v
	}
	if v.CanAddr() {
		return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
	}
	return v
}
