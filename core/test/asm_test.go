package test

import "testing"

func print1() {
	println("Hello-World")
}

func TestAsm(t *testing.T) {
	if pc := GetPC(); pc == 0 {
		t.Fatal("GetPC returned zero")
	}
}

func TestBaseFunc(t *testing.T) {
	t.Run("GetPC", func(t *testing.T) {
		if pc := GetPC(); pc == 0 {
			t.Fatal("GetPC returned zero")
		}
	})
}
