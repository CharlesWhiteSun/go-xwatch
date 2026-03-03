package cli

import "testing"

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	called := false
	reg.Register(CommandFunc{NameStr: "hello", Fn: func(args []string) error {
		called = true
		if len(args) != 2 || args[0] != "a" || args[1] != "b" {
			t.Fatalf("unexpected args: %#v", args)
		}
		return nil
	}})

	cmd, ok := reg.Get("hello")
	if !ok {
		t.Fatalf("command not found")
	}
	if err := cmd.Run([]string{"a", "b"}); err != nil {
		t.Fatalf("run error: %v", err)
	}
	if !called {
		t.Fatalf("command not executed")
	}
}

func TestRegistryNames(t *testing.T) {
	reg := NewRegistry(CommandFunc{NameStr: "a", Fn: func([]string) error { return nil }}, CommandFunc{NameStr: "b", Fn: func([]string) error { return nil }})
	names := reg.Names()
	if len(names) != 2 {
		t.Fatalf("unexpected names length: %d", len(names))
	}
}
