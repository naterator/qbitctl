package main

import "testing"

func TestRootExposesCAFileOverride(t *testing.T) {
	root := newRootCmd()
	flag := root.PersistentFlags().Lookup("ca-file")
	if flag == nil {
		t.Fatal("root command is missing --ca-file")
	}
	if flag.DefValue != "" {
		t.Fatalf("--ca-file default = %q, want empty", flag.DefValue)
	}
}
