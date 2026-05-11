package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
)

var (
	funcNames   FuncNames
	packageName string
	verbose     bool
)

type FuncNames []string

func (f *FuncNames) String() string {
	return ""
}

func (f *FuncNames) Set(name string) error {
	*f = append(*f, name)
	return nil
}

type Node struct {
	Val  string
	Type string
	Cmd  string
}

type Scope struct {
	PkgName string
	Func    *ast.FuncDecl
	Nodes   []*Node
}

func filterDuplicates(in <-chan []*Node) <-chan *Node {
	out := make(chan *Node)
	go func() {
		defer close(out)
		seen := make(map[string]string)
		for stmt := range in {
			for _, s := range stmt {
				if _type, found := seen[s.Val]; found && _type == s.Type {
					continue
				}
				seen[s.Val] = s.Type
				out <- s
			}
		}
	}()
	return out
}

func filterTypes(in <-chan *Node) <-chan *Node {
	out := make(chan *Node)
	go func() {
		defer close(out)
		for stmt := range in {
			if stmt.Type != "*ast.BasicLit" &&
				stmt.Type != "TODO" &&
				stmt.Val != "" &&
				stmt.Val != "TODO" {
				out <- stmt
			}
		}
	}()
	return out
}

func getExpression(e ast.Expr) any {
	switch v := e.(type) {
	case *ast.BasicLit:
		//		return v.Value
	case *ast.BinaryExpr:
		return getExpression(v.X)
	case *ast.CallExpr:
		//		return getExpression(v.Fun)
	case *ast.Ident:
		return v.Name
	case *ast.IndexExpr:
		return fmt.Sprintf("%%v %s[%s]\n", getExpression(v.X), v.Index)
	case *ast.FuncLit:
		return v.Body
	}
	return ""
}

func getFuncParams(scope *Scope) <-chan *Scope {
	out := make(chan *Scope)
	go func() {
		defer close(out)
		for _, field := range scope.Func.Type.Params.List {
			for _, fieldName := range field.Names {
				scope.Nodes = append(
					scope.Nodes,
					&Node{
						Val:  fieldName.Name,
						Type: fmt.Sprintf("%T", fieldName),
					},
				)
			}
		}
		out <- scope
	}()
	return out
}

func getNodes(in <-chan *Scope) <-chan []*Node {
	out := make(chan []*Node)
	go func() {
		defer close(out)
		for scope := range in {
			out <- getStatements(scope.Func.Body.List, scope.Nodes)
		}
	}()
	return out
}

func getStatements(list []ast.Stmt, nodes []*Node) []*Node {
	for _, stmt := range list {
		switch v := stmt.(type) {
		case *ast.AssignStmt:
			for _, e := range v.Lhs {
				nodes = append(nodes, &Node{
					Val:  getExpression(e).(string),
					Type: fmt.Sprintf("%T", e),
				})
			}
			for _, e := range v.Rhs {
				nodes = append(nodes, &Node{
					Val:  getExpression(e).(string),
					Type: fmt.Sprintf("%T", e),
				})
			}
		case *ast.CaseClause:
			nodes = getStatements(v.Body, nodes)
		case *ast.DeclStmt:
			generalDeclarationNodes := v.Decl.(*ast.GenDecl)
			for _, spec := range generalDeclarationNodes.Specs {
				val := spec.(*ast.ValueSpec)
				for _, name := range val.Names {
					nodes = append(nodes, &Node{
						Val:  name.Name,
						Type: "TODO",
					})
				}
			}
		case *ast.DeferStmt:
		case *ast.ForStmt:
			nodes = getStatements(v.Body.List, nodes)
		case *ast.GoStmt:
			nodes = getStatements(getExpression(v.Call).(*ast.BlockStmt).List, nodes)
		case *ast.IfStmt:
			nodes = getStatements(v.Body.List, nodes)
			nodes = append(nodes, &Node{
				Val:  getExpression(v.Cond).(string),
				Type: fmt.Sprintf("%T", v.Cond),
			})
		case *ast.RangeStmt:
			nodes = getStatements(v.Body.List, nodes)
		case *ast.ReturnStmt:
		case *ast.TypeSwitchStmt:
			nodes = getStatements(v.Body.List, nodes)
		}
	}
	return nodes
}

func outputCmd(in <-chan *Node) <-chan *Node {
	out := make(chan *Node)
	go func() {
		defer close(out)
		for stmt := range in {
			stmt.Cmd = fmt.Sprintf("display -a %s\n", stmt.Val)
			out <- stmt
		}
	}()
	return out
}

func pipeline(scope *Scope) <-chan *Node {
	return outputCmd(
		filterTypes(
			filterDuplicates(
				getNodes(
					getFuncParams(scope),
				),
			),
		),
	)
}

func main() {
	flag.Var(&funcNames, "func", "The name of the function to get the symbols.")
	flag.StringVar(&packageName, "package", "main", "The name of the package to get the symbols.")
	flag.BoolVar(&verbose, "verbose", false, "Print debug information.")
	flag.Parse()

	fs := token.NewFileSet()
	cwd, _ := os.Getwd()
	pkgs, _ := parser.ParseDir(fs, cwd, nil, parser.SkipObjectResolution)

	var scopes []*Scope
	for pkgName, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				switch v := decl.(type) {
				case *ast.FuncDecl:
					if slices.Contains(funcNames, v.Name.Name) {
						scopes = append(scopes, &Scope{
							PkgName: pkgName,
							Func:    v,
						})
					}
				}
			}
		}
	}

	for _, scope := range scopes {
		fmt.Printf("break %s.%s\n", scope.PkgName, scope.Func.Name.Name)
		for stmt := range pipeline(scope) {
			fmt.Print(stmt.Cmd)
		}
	}
}
