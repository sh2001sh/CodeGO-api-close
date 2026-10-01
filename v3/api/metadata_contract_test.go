package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// Metadata routes are concatenated in range loops, so the literal route scan
// cannot see them. Expand the declaration's actual prefix/end expressions;
// unknown syntax fails visibly rather than dropping routes from coverage.
func TestContractCoversDynamicMetadataDeclarations(t *testing.T) {
	const source = "../internal/catalogcontrol/metadata_routes.go"
	file, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	paths := contractPaths(t)
	var expression func(ast.Expr, map[string]string) string
	expression = func(node ast.Expr, values map[string]string) string {
		switch n := node.(type) {
		case *ast.BasicLit:
			value, err := strconv.Unquote(n.Value)
			if err != nil {
				t.Fatal(err)
			}
			return value
		case *ast.Ident:
			value, exists := values[n.Name]
			if !exists {
				t.Fatalf("unknown metadata route variable %s", n.Name)
			}
			return value
		case *ast.BinaryExpr:
			if n.Op == token.ADD {
				return expression(n.X, values) + expression(n.Y, values)
			}
		}
		t.Fatalf("unsupported metadata route expression %T", node)
		return ""
	}
	count := 0
	var block func(*ast.BlockStmt, map[string]string)
	block = func(body *ast.BlockStmt, values map[string]string) {
		for _, statement := range body.List {
			switch n := statement.(type) {
			case *ast.AssignStmt:
				if name, ok := n.Lhs[0].(*ast.Ident); ok {
					values[name.Name] = expression(n.Rhs[0], values)
					continue
				}
				index, ok := n.Lhs[0].(*ast.IndexExpr)
				if !ok {
					t.Fatalf("unsupported metadata route assignment %T", n.Lhs[0])
				}
				pattern := strings.ReplaceAll(expression(index.Index, values), "{$}", "")
				method, path, _ := strings.Cut(pattern, " ")
				operation := paths[path][strings.ToLower(method)]
				if operation.OperationID == "" {
					t.Errorf("%s declares %s without a generated operation", source, pattern)
				} else if operation.RequiredRole != "admin" {
					t.Errorf("metadata administration %s lacks administrator scope", pattern)
				}
				count++
			case *ast.RangeStmt:
				items, ok := n.X.(*ast.CompositeLit)
				if !ok {
					t.Fatalf("unsupported metadata prefixes %T", n.X)
				}
				for _, item := range items.Elts {
					next := map[string]string{}
					for key, value := range values {
						next[key] = value
					}
					next[n.Value.(*ast.Ident).Name] = expression(item, values)
					block(n.Body, next)
				}
			case *ast.IfStmt:
				condition, ok := n.Cond.(*ast.BinaryExpr)
				if !ok || (condition.Op != token.EQL && condition.Op != token.NEQ) || n.Else != nil {
					t.Fatal("unsupported metadata prefix condition")
				}
				equal := expression(condition.X, values) == expression(condition.Y, values)
				if equal == (condition.Op == token.EQL) {
					block(n.Body, values)
				}
			default:
				t.Fatalf("unsupported metadata registration statement %T", statement)
			}
		}
	}
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Name.Name == "registerMetadataRoutes" {
			block(fn.Body, map[string]string{})
		}
	}
	if count == 0 {
		t.Fatal("metadata route registration was not inspected")
	}
}
