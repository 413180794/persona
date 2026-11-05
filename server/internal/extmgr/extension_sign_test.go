package extmgr

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A Mac program bun left unsigned is signed ad hoc; one that cannot be signed is an error that says to upgrade bun
// (#2555); without codesign (not a Mac) nothing is checked.
func TestSignedForMac(t *testing.T) {
	defer func(c func(context.Context, ...string) error) { codesign = c }(codesign)
	ctx := context.Background()
	run := func(verify []error, sign error) (error, []string) {
		var calls []string
		codesign = func(_ context.Context, args ...string) error {
			calls = append(calls, strings.Join(args[:len(args)-1], " "))
			if args[0] == "-v" {
				err := verify[0]
				verify = verify[1:]
				return err
			}
			return sign
		}
		return signedForMac(ctx, "/x/bin/demo-darwin-arm64"), calls
	}
	bad := errors.New("code object is not signed at all")

	if err, calls := run([]error{nil}, nil); err != nil || len(calls) != 1 {
		t.Fatalf("signed: %v %v", err, calls)
	}
	if err, calls := run([]error{errNoCodesign}, nil); err != nil || len(calls) != 1 {
		t.Fatalf("no codesign: %v %v", err, calls)
	}
	if err, calls := run([]error{bad, nil}, nil); err != nil || strings.Join(calls, "|") != "-v|--force --sign -|-v" {
		t.Fatalf("signed ad hoc: %v %v", err, calls)
	}
	err, _ := run([]error{bad}, errors.New("invalid or unsupported format for signature"))
	if !errors.Is(err, errBunUnsigned) || !strings.Contains(err.Error(), "bun upgrade") || !strings.Contains(err.Error(), "demo-darwin-arm64") {
		t.Fatalf("cannot be signed: %v", err)
	}
}
