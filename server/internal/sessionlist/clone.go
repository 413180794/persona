package sessionlist

import (
	"bytes"
	"encoding/json"
	"reflect"
)

// clone detaches transport payloads while preserving concrete adapter types.
// Unexported state (for example time.Time's location) is copied by value and must
// remain immutable. Cycles are preserved, although JSON transport cannot encode
// cyclic values. Byte slices, including json.RawMessage, use a single memcopy.
func clone(value any) any {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case json.RawMessage:
		return json.RawMessage(bytes.Clone(v))
	case []byte:
		return bytes.Clone(v)
	}
	return cloneValue(reflect.ValueOf(value), make(map[cloneVisit]reflect.Value)).Interface()
}

type cloneVisit struct {
	typeOf  reflect.Type
	pointer uintptr
	length  int
}

func cloneValue(v reflect.Value, seen map[cloneVisit]reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(cloneValue(v.Elem(), seen))
		return out
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		key := cloneVisit{typeOf: v.Type(), pointer: v.Pointer()}
		if out, exists := seen[key]; exists {
			return out
		}
		out := reflect.New(v.Type().Elem())
		seen[key] = out
		out.Elem().Set(cloneValue(v.Elem(), seen))
		return out
	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		key := cloneVisit{typeOf: v.Type(), pointer: uintptr(v.UnsafePointer())}
		if out, exists := seen[key]; exists {
			return out
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		seen[key] = out
		it := v.MapRange()
		for it.Next() {
			out.SetMapIndex(cloneValue(it.Key(), seen), cloneValue(it.Value(), seen))
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		key := cloneVisit{typeOf: v.Type(), pointer: v.Pointer(), length: v.Len()}
		if out, exists := seen[key]; exists {
			return out
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		seen[key] = out
		if v.Type().Elem().Kind() == reflect.Uint8 {
			reflect.Copy(out, v)
		} else {
			for n := 0; n < v.Len(); n++ {
				out.Index(n).Set(cloneValue(v.Index(n), seen))
			}
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for n := 0; n < v.NumField(); n++ {
			if out.Field(n).CanSet() && v.Field(n).CanInterface() {
				out.Field(n).Set(cloneValue(v.Field(n), seen))
			}
		}
		return out
	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		for n := 0; n < v.Len(); n++ {
			out.Index(n).Set(cloneValue(v.Index(n), seen))
		}
		return out
	default:
		return v
	}
}
