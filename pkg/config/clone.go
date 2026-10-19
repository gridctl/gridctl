package config

import (
	"reflect"
	"time"
)

// Clone returns a deep copy. Nil stays nil. A pointer to a zero value stays
// a non-nil pointer, and an empty slice stays distinct from a nil slice.
// Unexported fields are copied too. A nil stack returns nil.
func (s *Stack) Clone() *Stack {
	if s == nil {
		return nil
	}
	copied := deepCopy(reflect.ValueOf(s))
	out, ok := copied.Interface().(*Stack)
	if !ok || out == nil {
		return nil
	}
	// Struct assignment copies these slice headers. Replace them so a later
	// append on the clone cannot mutate the source.
	out.overrideWarnings = append([]string(nil), s.overrideWarnings...)
	out.emptySecretSites = append([]Consumer(nil), s.emptySecretSites...)
	if s.Secrets != nil && out.Secrets != nil {
		for i := range s.Secrets.Sets {
			if i < len(out.Secrets.Sets) {
				out.Secrets.Sets[i].scopeDeclared = s.Secrets.Sets[i].scopeDeclared
			}
		}
	}
	return out
}

func deepCopy(v reflect.Value) reflect.Value {
	c := copier{seen: map[uintptr]reflect.Value{}}
	return c.value(v)
}

type copier struct {
	seen map[uintptr]reflect.Value
}

func (c *copier) value(v reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	switch v.Kind() {
	case reflect.Ptr:
		return c.pointer(v)
	case reflect.Slice:
		return c.slice(v)
	case reflect.Map:
		return c.mapping(v)
	case reflect.Struct:
		return c.structure(v)
	case reflect.Array:
		return c.array(v)
	case reflect.Interface:
		return c.iface(v)
	default:
		return v
	}
}

func (c *copier) pointer(v reflect.Value) reflect.Value {
	if v.IsNil() {
		return reflect.Zero(v.Type())
	}
	addr := v.Pointer()
	if prev, ok := c.seen[addr]; ok {
		return prev
	}
	out := reflect.New(v.Type().Elem())
	c.seen[addr] = out
	copied := c.value(v.Elem())
	if copied.IsValid() && copied.Type().AssignableTo(out.Elem().Type()) {
		out.Elem().Set(copied)
	}
	return out
}

func (c *copier) slice(v reflect.Value) reflect.Value {
	if v.IsNil() {
		return reflect.Zero(v.Type())
	}
	out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
	for i := 0; i < v.Len(); i++ {
		copied := c.value(v.Index(i))
		if copied.IsValid() && copied.Type().AssignableTo(out.Index(i).Type()) {
			out.Index(i).Set(copied)
		}
	}
	return out
}

func (c *copier) mapping(v reflect.Value) reflect.Value {
	if v.IsNil() {
		return reflect.Zero(v.Type())
	}
	out := reflect.MakeMapWithSize(v.Type(), v.Len())
	iter := v.MapRange()
	for iter.Next() {
		out.SetMapIndex(c.value(iter.Key()), c.value(iter.Value()))
	}
	return out
}

func (c *copier) structure(v reflect.Value) reflect.Value {
	out := reflect.New(v.Type()).Elem()
	// Assignment copies unexported fields, including time.Time's location
	// pointer. Exported composites are replaced below so they are not shared.
	out.Set(v)
	if v.Type() == reflect.TypeOf(time.Time{}) {
		return out
	}
	for i := 0; i < v.NumField(); i++ {
		dst := out.Field(i)
		if !dst.CanSet() {
			continue
		}
		src := v.Field(i)
		switch src.Kind() {
		case reflect.Ptr, reflect.Slice, reflect.Map, reflect.Struct, reflect.Array, reflect.Interface:
			copied := c.value(src)
			if copied.IsValid() && copied.Type().AssignableTo(dst.Type()) {
				dst.Set(copied)
			}
		}
	}
	return out
}

func (c *copier) array(v reflect.Value) reflect.Value {
	out := reflect.New(v.Type()).Elem()
	for i := 0; i < v.Len(); i++ {
		copied := c.value(v.Index(i))
		if copied.IsValid() && copied.Type().AssignableTo(out.Index(i).Type()) {
			out.Index(i).Set(copied)
		}
	}
	return out
}

func (c *copier) iface(v reflect.Value) reflect.Value {
	if v.IsNil() {
		return reflect.Zero(v.Type())
	}
	elem := c.value(v.Elem())
	out := reflect.New(v.Type()).Elem()
	if elem.IsValid() && elem.Type().AssignableTo(v.Type()) {
		out.Set(elem)
	}
	return out
}
