package compiler

import (
	"fmt"
	"reflect"
	"sort"
	"unsafe"
)

type jitFieldStep struct {
	offset  uintptr
	pointer bool
	typ     reflect.Type
}

type jitStructField struct {
	indexPath []int
	path      []jitFieldStep
	name      string
	omitEmpty bool
	typ       reflect.Type
}

type jitFieldCandidate struct {
	indexPath []int
	name      string
	omitEmpty bool
	tagged    bool
	typ       reflect.Type
}

func jitExportedFields(typ reflect.Type) ([]jitStructField, error) {
	var candidates []jitFieldCandidate
	if err := collectJITFields(typ, nil, 0, &candidates, map[reflect.Type]bool{typ: true}); err != nil {
		return nil, err
	}

	byName := make(map[string][]jitFieldCandidate, len(candidates))
	for _, candidate := range candidates {
		byName[candidate.name] = append(byName[candidate.name], candidate)
	}
	selected := make([]jitFieldCandidate, 0, len(byName))
	for _, fields := range byName {
		dominant, ok := dominantJITField(fields)
		if ok {
			selected = append(selected, dominant)
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		return compareJITIndex(selected[i].indexPath, selected[j].indexPath) < 0
	})

	result := make([]jitStructField, 0, len(selected))
	for _, candidate := range selected {
		path, err := buildJITFieldPath(typ, candidate.indexPath)
		if err != nil {
			return nil, err
		}
		result = append(result, jitStructField{
			indexPath: candidate.indexPath,
			path:      path,
			name:      candidate.name,
			omitEmpty: candidate.omitEmpty,
			typ:       candidate.typ,
		})
	}
	return result, nil
}

func collectJITFields(
	typ reflect.Type,
	indexPath []int,
	depth int,
	candidates *[]jitFieldCandidate,
	ancestry map[reflect.Type]bool,
) error {
	if depth > 64 {
		return fmt.Errorf("json: embedded field depth exceeds 64 at %s", typ)
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldType := field.Type
		isPointer := fieldType.Kind() == reflect.Pointer
		if isPointer {
			fieldType = fieldType.Elem()
		}
		if field.PkgPath != "" && (!field.Anonymous || fieldType.Kind() != reflect.Struct) {
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, omitEmpty, supported := jitJSONField(field)
		if !supported {
			return fmt.Errorf("json: JIT encoder does not support field option on %s", field.Name)
		}
		index := append(append([]int(nil), indexPath...), i)
		tagged := name != field.Name
		if name == "" {
			name = field.Name
		}
		if field.Anonymous && !tagged && fieldType.Kind() == reflect.Struct {
			if ancestry[fieldType] {
				continue
			}
			nextAncestry := make(map[reflect.Type]bool, len(ancestry)+1)
			for typ := range ancestry {
				nextAncestry[typ] = true
			}
			nextAncestry[fieldType] = true
			if err := collectJITFields(fieldType, index, depth+1, candidates, nextAncestry); err != nil {
				return err
			}
			continue
		}
		*candidates = append(*candidates, jitFieldCandidate{
			indexPath: index,
			name:      name,
			omitEmpty: omitEmpty,
			tagged:    tagged,
			typ:       field.Type,
		})
	}
	return nil
}

func dominantJITField(fields []jitFieldCandidate) (jitFieldCandidate, bool) {
	minDepth := len(fields[0].indexPath)
	for _, field := range fields[1:] {
		if len(field.indexPath) < minDepth {
			minDepth = len(field.indexPath)
		}
	}
	var shallow []jitFieldCandidate
	tagged := 0
	for _, field := range fields {
		if len(field.indexPath) != minDepth {
			continue
		}
		shallow = append(shallow, field)
		if field.tagged {
			tagged++
		}
	}
	if len(shallow) == 1 {
		return shallow[0], true
	}
	if tagged == 1 {
		for _, field := range shallow {
			if field.tagged {
				return field, true
			}
		}
	}
	return jitFieldCandidate{}, false
}

func compareJITIndex(left, right []int) int {
	for i := 0; i < len(left) && i < len(right); i++ {
		switch {
		case left[i] < right[i]:
			return -1
		case left[i] > right[i]:
			return 1
		}
	}
	switch {
	case len(left) < len(right):
		return -1
	case len(left) > len(right):
		return 1
	default:
		return 0
	}
}

func buildJITFieldPath(root reflect.Type, indexPath []int) ([]jitFieldStep, error) {
	current := root
	path := make([]jitFieldStep, 0, len(indexPath))
	for position, index := range indexPath {
		if index < 0 || index >= current.NumField() {
			return nil, fmt.Errorf("json: invalid field path in %s", root)
		}
		field := current.Field(index)
		step := jitFieldStep{offset: field.Offset, typ: field.Type}
		if position == len(indexPath)-1 {
			path = append(path, step)
			return path, nil
		}
		if field.Type.Kind() == reflect.Pointer {
			step.pointer = true
			step.typ = field.Type.Elem()
			current = field.Type.Elem()
		} else {
			current = field.Type
		}
		path = append(path, step)
	}
	return nil, fmt.Errorf("json: empty field path in %s", root)
}

func jitResolveField(root unsafe.Pointer, path []jitFieldStep, create bool) (unsafe.Pointer, bool) {
	base := root
	for _, step := range path {
		address := unsafe.Add(base, step.offset)
		if !step.pointer {
			base = address
			continue
		}
		pointer := *(*unsafe.Pointer)(address)
		if pointer == nil {
			if !create {
				return nil, false
			}
			pointer = runtimeMallocGC(step.typ.Size(), unsafe.Pointer(typePointer(step.typ)), true)
			*(*unsafe.Pointer)(address) = pointer
		}
		base = pointer
	}
	return base, true
}

func jitFieldStaticOffset(path []jitFieldStep) (uintptr, bool) {
	var offset uintptr
	for _, step := range path {
		if step.pointer {
			return 0, false
		}
		offset += step.offset
	}
	return offset, true
}
