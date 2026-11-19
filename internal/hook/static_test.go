package hook

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Only a keyed `Code: 2` literal is caught; a positional literal or a computed code is not.
// TestNoBareBlockCodeInCoreHooks: a core hook blocks only through hook.Block, hook.BlockTagged or
// hook.CouldNotCheck, so every block carries its
// reason tag or the COULD NOT CHECK token. A literal Code: 2 anywhere else is a block that
// escapes the declared reasons.
func TestNoBareBlockCodeInCoreHooks(t *testing.T) {
	fset := token.NewFileSet()
	for _, dir := range []string{"../hooks", "../commitgate", "../codelang"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatalf("parse %s/%s: %v", dir, name, err)
			}
			checkBareBlock(t, fset, file)
		}
	}
}

func checkBareBlock(t *testing.T, fset *token.FileSet, file *ast.File) {
	t.Helper()
	for _, decl := range file.Decls {
		fn := ""
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fn = fd.Name.Name
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			val, isLit := kv.Value.(*ast.BasicLit)
			if ok && key.Name == "Code" && isLit && val.Kind == token.INT && val.Value == "2" {
				t.Errorf("%s: bare Code: 2 in %q; use hook.Block, hook.BlockTagged or hook.CouldNotCheck", fset.Position(kv.Pos()), fn)
			}
			return true
		})
	}
}
