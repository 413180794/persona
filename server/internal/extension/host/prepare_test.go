package host

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestPreparedResourcesAlwaysDisposed(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		failure       bool
	}{
		{"exit", "/bin/true", false}, {"start-failure", "/no-such-prepared-command", true}, {"prepare-failure", "/bin/true", true}, {"stopped", "/bin/sh", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var disposed atomic.Int32
			p := &Proc{spec: Spec{Command: "/bin/true", Prepare: func(string, []string) (string, []string, []string, []*os.File, func(), error) {
				var err error
				if tc.name == "prepare-failure" {
					err = errors.New("prepare failed")
				}
				var args []string
				if tc.name == "stopped" {
					args = []string{"-c", "exec sleep 30"}
				}
				return tc.command, args, nil, nil, func() { disposed.Add(1) }, err
			}}}
			ch, _, err := p.spawn(context.Background())
			if tc.failure {
				if err == nil || disposed.Load() != 1 {
					t.Fatal(err, disposed.Load())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "stopped" {
				p.end(ch, nil)
			}
			select {
			case <-ch.exited:
			case <-time.After(time.Second):
				t.Fatal("wrapper did not exit")
			}
			if disposed.Load() != 1 {
				t.Fatal("resources not disposed exactly once", disposed.Load())
			}
		})
	}
}
