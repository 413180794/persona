package main

import (
	"strings"
	"testing"
)

func TestCheckNodeStdioArgs(t *testing.T) {
	for _, args := range [][]string{nil, {"--stdio"}} {
		if err := checkNodeStdioArgs(args); err != nil {
			t.Errorf("%q: %v", args, err)
		}
	}
	for _, args := range [][]string{{"doctr"}, {"--atach"}, {"--stdio", "x"}, {"-stdio"}} {
		err := checkNodeStdioArgs(args)
		if err == nil || !strings.Contains(err.Error(), "不认识的 node 子命令") || !strings.Contains(err.Error(), "doctor") {
			t.Errorf("%q: %v", args, err)
		}
	}
}
