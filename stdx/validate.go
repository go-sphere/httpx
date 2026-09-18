package stdx

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/go-playground/validator/v10"
)

var structValidator = newStructValidator()

func newStructValidator() *validator.Validate {
	v := validator.New()
	// Match gin's convention so the same DTO validates identically on every
	// adapter (Binder contract: `binding:"required"` etc.).
	v.SetTagName("binding")
	return v
}

// validateStruct runs Binder-contract validation after a successful decode.
// It mirrors gin's validator semantics: structs and pointers to structs are
// validated, and every element of a slice or array is validated individually
// (matching gin's SliceValidationError accumulation). Other kinds pass through.
func validateStruct(obj any) error {
	if obj == nil {
		return nil
	}
	v := reflect.ValueOf(obj)
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		if v.Elem().Kind() != reflect.Struct {
			// Unwrap pointer chains such as **T: validator.Struct only
			// accepts one level, so passing them through would reject a
			// valid target with a 400.
			return validateStruct(v.Elem().Interface())
		}
		if !needsValidation(v.Type().Elem()) {
			return nil
		}
		return structValidator.Struct(obj)
	case reflect.Struct:
		if !needsValidation(v.Type()) {
			return nil
		}
		return structValidator.Struct(obj)
	case reflect.Slice, reflect.Array:
		var errs []error
		for i := range v.Len() {
			elem := v.Index(i)
			if elem.Kind() == reflect.Pointer && elem.IsNil() {
				// A null element cannot satisfy the element type's rules and
				// would nil-dereference in the handler, so it is reported like
				// any other validation failure. gin's validator panics on this
				// input, so rejecting it is also what keeps the adapters in
				// parity. A nil *target* is different: there is nothing to
				// validate, which the Pointer case above allows.
				errs = append(errs, fmt.Errorf("element %d is null", i))
				continue
			}
			if err := validateStruct(elem.Interface()); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	default:
		return nil
	}
}

// validationNeeded caches, per struct type, whether any `binding` tag is
// reachable from it. Most DTOs a handler binds carry none, and for those the
// validator's full reflective walk (and its allocation) would find nothing.
var validationNeeded sync.Map // reflect.Type → bool

func needsValidation(t reflect.Type) bool {
	if cached, ok := validationNeeded.Load(t); ok {
		return cached.(bool)
	}
	need := hasBindingTag(t, map[reflect.Type]bool{})
	validationNeeded.Store(t, need)
	return need
}

// hasBindingTag walks t through pointers, slices, arrays and maps into every
// struct it can reach. It reports a tag on any field, exported or not, and on
// any nested type regardless of whether the validator would dive into it —
// a superset, so "no tag anywhere" is exactly the case where the validator
// has nothing to check.
func hasBindingTag(t reflect.Type, seen map[reflect.Type]bool) bool {
	for {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			t = t.Elem()
			continue
		}
		break
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return false
	}
	seen[t] = true
	for i := range t.NumField() {
		f := t.Field(i)
		if tag := f.Tag.Get("binding"); tag != "" && tag != "-" {
			return true
		}
		if hasBindingTag(f.Type, seen) {
			return true
		}
	}
	return false
}
