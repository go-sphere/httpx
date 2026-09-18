package stdx

import (
	"strings"
	"testing"
)

type requiredName struct {
	Name string `binding:"required"`
}

type untagged struct {
	Name string
	Age  int
}

type nested struct {
	Inner requiredName
}

type nestedPtr struct {
	Inner *requiredName
}

type withDive struct {
	Items []requiredName `binding:"dive"`
}

type withMap struct {
	Items map[string]requiredName `binding:"dive"`
}

// selfRef is a recursive type: anything that walks the type graph looking for
// tags has to terminate on it, with and without a tag present.
type selfRef struct {
	Next *selfRef
	Name string `binding:"required"`
}

type selfRefUntagged struct {
	Next     *selfRefUntagged
	Siblings []selfRefUntagged
	Name     string
}

type embedsTagged struct {
	requiredName
	Extra string
}

func TestValidateStruct(t *testing.T) {
	for _, tc := range []struct {
		name    string
		obj     any
		wantErr bool
		contain string
	}{
		{name: "Nil", obj: nil},
		{name: "NilPointer", obj: (*requiredName)(nil)},
		{name: "ValidPointer", obj: &requiredName{Name: "x"}},
		{name: "InvalidPointer", obj: &requiredName{}, wantErr: true},
		{name: "ValidValue", obj: requiredName{Name: "x"}},
		{name: "InvalidValue", obj: requiredName{}, wantErr: true},
		{name: "PointerChainIsUnwrapped", obj: ptrTo(&requiredName{}), wantErr: true},
		{name: "UntaggedZeroValuePasses", obj: &untagged{}},
		{name: "NestedStructIsValidated", obj: &nested{}, wantErr: true},
		{name: "NestedPointerNilIsAllowed", obj: &nestedPtr{}},
		{name: "NestedPointerIsValidated", obj: &nestedPtr{Inner: &requiredName{}}, wantErr: true},
		{name: "DiveIntoSlice", obj: &withDive{Items: []requiredName{{Name: "a"}, {}}}, wantErr: true},
		{name: "DiveIntoSliceValid", obj: &withDive{Items: []requiredName{{Name: "a"}}}},
		{name: "DiveIntoMap", obj: &withMap{Items: map[string]requiredName{"k": {}}}, wantErr: true},
		{name: "EmbeddedTag", obj: &embedsTagged{}, wantErr: true},
		{name: "EmbeddedTagValid", obj: &embedsTagged{requiredName: requiredName{Name: "x"}}},
		{name: "RecursiveTypeTerminates", obj: &selfRef{Name: "a", Next: &selfRef{}}, wantErr: true},
		{name: "RecursiveTypeValid", obj: &selfRef{Name: "a", Next: &selfRef{Name: "b"}}},
		{name: "RecursiveUntaggedTerminates", obj: &selfRefUntagged{Next: &selfRefUntagged{}}},
		{name: "SliceOfStructs", obj: []requiredName{{Name: "a"}, {}}, wantErr: true},
		{name: "SliceOfValidStructs", obj: []requiredName{{Name: "a"}, {Name: "b"}}},
		{name: "SliceOfPointers", obj: []*requiredName{{Name: "a"}, {}}, wantErr: true},
		{name: "SliceWithNullElement", obj: []*requiredName{{Name: "a"}, nil}, wantErr: true, contain: "element 1 is null"},
		{name: "ArrayOfStructs", obj: [2]requiredName{{Name: "a"}, {}}, wantErr: true},
		{name: "SliceOfScalars", obj: []int{0, 1}},
		{name: "Scalar", obj: 42},
		{name: "String", obj: "text"},
		{name: "Map", obj: map[string]requiredName{"k": {}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStruct(tc.obj)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateStruct(%T) = %v, wantErr %v", tc.obj, err, tc.wantErr)
			}
			if tc.contain != "" && !strings.Contains(err.Error(), tc.contain) {
				t.Fatalf("error %q does not mention %q", err, tc.contain)
			}
		})
	}
}

// Every element of a slice is reported, not just the first failure, matching
// gin's accumulated slice validation errors.
func TestValidateStructReportsEveryElement(t *testing.T) {
	err := validateStruct([]*requiredName{{}, nil, {}})
	if err == nil {
		t.Fatal("no error")
	}
	msg := err.Error()
	if strings.Count(msg, "'required' tag") != 2 || !strings.Contains(msg, "element 1 is null") {
		t.Fatalf("error = %q, want two required failures and the null element", msg)
	}
}

func ptrTo[T any](v T) *T { return &v }
