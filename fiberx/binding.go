package fiberx

import (
	"mime/multipart"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/valyala/fasthttp"
)

type structPlanKey struct {
	typ reflect.Type
	tag string
}

type explicitStructPlan struct {
	caseInsensitive bool
	hasExplicit     bool
	hasUntagged     bool
	allowedKeys     map[string]struct{}
	untaggedIndices [][]int
}

type savedField struct {
	idx   []int
	val   reflect.Value
	valid bool
}

var structPlanCache sync.Map // map[structPlanKey]*explicitStructPlan

func structBindPlan(dst any, tag string) *explicitStructPlan {
	rv := reflect.ValueOf(dst)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return nil
	}
	rt := rv.Type().Elem()
	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		return nil
	}
	key := structPlanKey{typ: rt, tag: tag}
	if cached, ok := structPlanCache.Load(key); ok {
		return cached.(*explicitStructPlan)
	}
	plan := buildExplicitStructPlan(rt, tag)
	actual, _ := structPlanCache.LoadOrStore(key, plan)
	return actual.(*explicitStructPlan)
}

func buildExplicitStructPlan(typ reflect.Type, tag string) *explicitStructPlan {
	p := &explicitStructPlan{
		caseInsensitive: tag == "header",
		allowedKeys:     make(map[string]struct{}),
	}
	visited := map[reflect.Type]bool{typ: true}
	var walk func(t reflect.Type, prefix []int)
	walk = func(t reflect.Type, prefix []int) {
		for i := range t.NumField() {
			sf := t.Field(i)
			if !sf.Anonymous && sf.PkgPath != "" {
				continue
			}
			idx := append(slices.Clone(prefix), i)
			raw, ok := sf.Tag.Lookup(tag)
			if sf.Anonymous {
				ft := sf.Type
				for ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				if raw == "-" {
					continue
				}
				if !ok && ft.Kind() == reflect.Struct {
					if !visited[ft] {
						visited[ft] = true
						walk(ft, idx)
						delete(visited, ft)
					}
					continue
				}
			}
			if !ok || raw == "-" || raw == "" {
				p.hasUntagged = true
				p.untaggedIndices = append(p.untaggedIndices, idx)
				continue
			}
			name, _, _ := strings.Cut(raw, ",")
			if name == "-" {
				p.hasUntagged = true
				p.untaggedIndices = append(p.untaggedIndices, idx)
				continue
			}
			if name == "" {
				name = sf.Name
			}
			if p.caseInsensitive {
				name = strings.ToLower(name)
			}
			p.hasExplicit = true
			p.allowedKeys[name] = struct{}{}
		}
	}
	walk(typ, nil)
	return p
}

func (p *explicitStructPlan) allows(key string) bool {
	if p.caseInsensitive {
		key = strings.ToLower(key)
	}
	if _, ok := p.allowedKeys[key]; ok {
		return true
	}
	if idx := strings.IndexAny(key, ".["); idx > 0 {
		if _, ok := p.allowedKeys[key[:idx]]; ok {
			return true
		}
	}
	return false
}

func (p *explicitStructPlan) filterValues(src map[string][]string) map[string][]string {
	out := make(map[string][]string, len(p.allowedKeys))
	for k, vals := range src {
		if p.allows(k) {
			out[k] = vals
		}
	}
	return out
}

func (p *explicitStructPlan) filterFiles(src map[string][]*multipart.FileHeader) map[string][]*multipart.FileHeader {
	out := make(map[string][]*multipart.FileHeader, len(p.allowedKeys))
	for k, files := range src {
		if p.allows(k) {
			out[k] = files
		}
	}
	return out
}

func (p *explicitStructPlan) snapshot(dst any) []savedField {
	root := reflect.ValueOf(dst)
	saved := make([]savedField, len(p.untaggedIndices))
	for i, idx := range p.untaggedIndices {
		fv, ok := fieldAtIndex(root, idx)
		if !ok || !fv.CanSet() {
			saved[i] = savedField{idx: idx}
			continue
		}
		cp := reflect.New(fv.Type()).Elem()
		cp.Set(fv)
		saved[i] = savedField{idx: idx, val: cp, valid: true}
	}
	return saved
}

func restoreUntagged(dst any, saved []savedField) {
	root := reflect.ValueOf(dst)
	for _, s := range saved {
		fv, ok := fieldAtIndex(root, s.idx)
		if !ok || !fv.CanSet() {
			continue
		}
		if s.valid {
			fv.Set(s.val)
		} else {
			fv.SetZero()
		}
	}
}

func fieldAtIndex(root reflect.Value, idx []int) (reflect.Value, bool) {
	cur := root
	for cur.Kind() == reflect.Pointer {
		if cur.IsNil() {
			return reflect.Value{}, false
		}
		cur = cur.Elem()
	}
	for _, step := range idx {
		if cur.Kind() == reflect.Pointer {
			if cur.IsNil() {
				return reflect.Value{}, false
			}
			cur = cur.Elem()
		}
		if cur.Kind() != reflect.Struct || step >= cur.NumField() {
			return reflect.Value{}, false
		}
		cur = cur.Field(step)
	}
	return cur, true
}

func isMultipartContentType(ct []byte) bool {
	mediaType, _, _ := strings.Cut(strings.TrimSpace(string(ct)), ";")
	return strings.EqualFold(strings.TrimSpace(mediaType), "multipart/form-data")
}

func filterFasthttpQueryArgs(src *fasthttp.Args, plan *explicitStructPlan, dst *fasthttp.Args) {
	for k, v := range src.All() {
		if plan.allows(string(k)) {
			dst.AddBytesKV(k, v)
		}
	}
}
