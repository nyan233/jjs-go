package container

import "testing"

func TestIteratorBounds(t *testing.T) {
	iter := NewIterator(3, true, func(index int) int {
		return index * 10
	}, func() {})

	if value, ok := iter.Forward(); ok || value != 0 {
		t.Fatalf("Forward at start returned (%d, %v)", value, ok)
	}
	for i := 0; i < 3; i++ {
		if !iter.Next() {
			t.Fatalf("expected item %d", i)
		}
		_ = iter.Take()
	}
	if iter.Next() {
		t.Fatal("iterator advanced past its tail")
	}
	if _, ok := iter.Index(3); ok {
		t.Fatal("Index accepted an out-of-range index")
	}
}

func TestSliceMapDeleteAndReuse(t *testing.T) {
	m := NewSliceMap[string, int](2)
	m.Store("a", 1)
	m.Store("b", 2)
	m.Delete("a")
	m.Store("c", 3)

	if _, ok := m.LoadOk("a"); ok {
		t.Fatal("deleted key was still present")
	}
	if value, ok := m.LoadOk("c"); !ok || value != 3 {
		t.Fatalf("got (%d, %v), want (3, true)", value, ok)
	}
	if m.Len() != 2 {
		t.Fatalf("got len %d, want 2", m.Len())
	}
}

func TestRWMutexMap(t *testing.T) {
	var m RWMutexMap[string, int]
	m.Store("answer", 42)
	if value, ok := m.LoadOk("answer"); !ok || value != 42 {
		t.Fatalf("got (%d, %v), want (42, true)", value, ok)
	}
	m.Delete("answer")
	if m.Len() != 0 {
		t.Fatalf("got len %d, want 0", m.Len())
	}
}

func TestSyncMap118Overwrite(t *testing.T) {
	var m SyncMap118[string, int]
	m.Store("answer", 41)
	m.Store("answer", 42)
	if value, ok := m.LoadOk("answer"); !ok || value != 42 {
		t.Fatalf("got (%d, %v), want (42, true)", value, ok)
	}
	if m.Len() != 1 {
		t.Fatalf("got len %d, want 1", m.Len())
	}
}
