package sourcevalidate

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"runtime"
	"runtime/debug"

	"github.com/nwverse/persona/server/internal/extension/sandbox"
	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/js"
)

// Run is the hidden child-only CLI dispatch. Both trusted launcher and child
// verify the kernel scope, or the explicitly authorized development policy and
// actual child rlimits. Never call Run/inspect from a manager path. No guest runs.
func Run(args []string, stdin io.Reader, stdout io.Writer) int {
	if len(args) != 2 || sandbox.VerifyValidationEntry(memoryMB, cpuPercent, args[0]) != nil {
		return 1
	}
	return run(args[1:], stdin, stdout)
}

func childControls() {
	// Runtime-specific defense in depth, never hard kernel process accounting.
	runtime.GOMAXPROCS(2)
	debug.SetMemoryLimit(96 << 20)
	debug.SetMaxStack(2 << 20)
	debug.SetMaxThreads(16)
}

func run(args []string, stdin io.Reader, stdout io.Writer) int {
	if len(args) != 1 || !validDigest(args[0]) {
		return 2
	}
	// Defense in depth only: neither cooperative Go limits nor panic recovery
	// replace the external kernel controls required by ValidationProcess.
	childControls()
	b, err := io.ReadAll(io.LimitReader(stdin, Most+1))
	if err != nil || len(b) == 0 || len(b) > Most {
		_, _ = io.WriteString(stdout, "reject\n")
		return 0
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != args[0] || !inspect(b) {
		_, _ = io.WriteString(stdout, "reject\n")
		return 0
	}
	_, _ = io.WriteString(stdout, "ok\n")
	return 0
}

// All AST allocation and structural traversal stays in this child. Parser error
// messages may contain source text; return only the fixed protocol verdict.
func inspect(b []byte) (accepted bool) {
	defer func() {
		if recover() != nil {
			accepted = false
		}
	}()
	ast, err := js.Parse(parse.NewInputBytes(b), js.Options{})
	if err != nil {
		return false
	}
	visitor := &imports{}
	js.Walk(visitor, ast)
	return !visitor.found
}

type imports struct{ found bool }

func (v *imports) Enter(node js.INode) js.IVisitor {
	if v.found {
		return nil
	}
	switch n := node.(type) {
	case *js.ImportStmt, *js.ImportMetaExpr:
		v.found = true
	case *js.ExportStmt:
		v.found = len(n.Module) > 0
	case *js.LiteralExpr:
		v.found = n.TokenType == js.ImportToken
	}
	if v.found {
		return nil
	}
	return v
}
func (*imports) Exit(js.INode) {}
