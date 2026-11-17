package tag

import "testing"

func TestCoerce(t *testing.T) {
	tg := New("x", "X", TypeFloat)
	v, err := tg.Coerce(5)
	if err != nil || v != 5.0 {
		t.Fatalf("got %v %v", v, err)
	}
	tg2 := New("b", "B", TypeBool)
	v2, _ := tg2.Coerce(1)
	if v2 != true {
		t.Fatalf("got %v", v2)
	}
}
