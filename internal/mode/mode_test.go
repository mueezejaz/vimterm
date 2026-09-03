package mode

import "testing"

func TestManager(t *testing.T) {
	m := NewManager()
	if !m.Is(ModeNormal) {
		t.Fatal("manager must start in normal mode")
	}
	if got := m.Current(); got != ModeNormal {
		t.Fatalf("current = %v, want normal", got)
	}
	m.Enter(ModeInsert)
	if !m.Is(ModeInsert) {
		t.Fatal("expected insert mode after Enter")
	}
	m.Enter(ModeNormal)
	if !m.Is(ModeNormal) {
		t.Fatal("expected normal mode after Enter")
	}
}

func TestString(t *testing.T) {
	cases := []struct {
		m    Mode
		want string
	}{
		{ModeNormal, "NORMAL"},
		{ModeInsert, "INSERT"},
	}
	for _, c := range cases {
		if got := c.m.String(); got != c.want {
			t.Errorf("Mode(%d).String() = %q, want %q", c.m, got, c.want)
		}
	}
}
