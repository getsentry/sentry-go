package sentry

import (
	"maps"
	"reflect"
	"slices"
)

func deepCopyValue(v any) any {
	if v == nil {
		return nil
	}
	if b, ok := v.([]byte); ok {
		return slices.Clone(b)
	}
	// Fast-paths using stdlib clones for common slice/map types.
	switch t := v.(type) {
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
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		newMap := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			k := iter.Key()
			val := iter.Value().Interface()
			newVal := deepCopyValue(val)
			newMap.SetMapIndex(k, reflect.ValueOf(newVal))
		}
		return newMap.Interface()
	case reflect.Slice:
		if rv.IsNil() {
			return reflect.Zero(rv.Type()).Interface()
		}
		n := rv.Len()
		newSlice := reflect.MakeSlice(rv.Type(), n, n)
		for i := 0; i < n; i++ {
			elem := rv.Index(i).Interface()
			newSlice.Index(i).Set(reflect.ValueOf(deepCopyValue(elem)))
		}
		return newSlice.Interface()
	case reflect.Array:
		n := rv.Len()
		newArr := reflect.New(rv.Type()).Elem()
		for i := 0; i < n; i++ {
			newArr.Index(i).Set(reflect.ValueOf(deepCopyValue(rv.Index(i).Interface())))
		}
		return newArr.Interface()
	case reflect.Pointer:
		if rv.IsNil() {
			return nil
		}
		newPtr := reflect.New(rv.Elem().Type())
		newPtr.Elem().Set(reflect.ValueOf(deepCopyValue(rv.Elem().Interface())))
		return newPtr.Interface()
	default:
		return v
	}
}

func deepCopyMapStringAny(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyContext(cxt Context) Context {
	if cxt == nil {
		return nil
	}
	return deepCopyMapStringAny(cxt)
}

func deepCopyMapStringString(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	return maps.Clone(m)
}

func deepCopyBreadcrumb(b *Breadcrumb) *Breadcrumb {
	if b == nil {
		return nil
	}
	c := *b
	if b.Data != nil {
		c.Data = deepCopyMapStringAny(b.Data)
	}
	return &c
}
