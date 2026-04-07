package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/types"
	"log"
	"os"
	"sort"

	"golang.org/x/tools/go/packages"
)

var (
	buf         *bytes.Buffer = bytes.NewBuffer(nil)
	funcName    string
	packageName string
	verbose     bool
)

func getSymbols(scope *types.Scope, pkgPath string) {
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		// FILTER: Skip imported packages
		if obj.Pkg() != nil && obj.Pkg().Path() != pkgPath {
			continue
		}
		switch v := obj.(type) {
		case *types.Var:
			kind := v.Kind().String()
			if kind == "PackageVar" {
				buf.WriteString(fmt.Sprintf("watch -w %s\n", name))
			}
		case *types.Func:
			if funcName == "" || name == funcName {
				buf.WriteString(fmt.Sprintf("break %s.%s\n", v.Pkg().Name(), name))
				parseFunc(v.Scope())
			}
		}
	}
	// Unfortunately, this will get duplicate vars.  It's necessary to do this,
	// but only in the scope of a function.
	//	for child := range scope.Children() {
	//		getSymbols(child, pkgPath)
	//	}
}

func parseFunc(scope *types.Scope) {
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if v, ok := obj.(*types.Var); ok {
			if v.Kind().String() == "LocalVar" {
				buf.WriteString(fmt.Sprintf("display -a %s\n", v.Name()))
			}
		}
	}
	for child := range scope.Children() {
		parseFunc(child)
	}
}

func main() {
	flag.StringVar(&funcName, "func", "", "The name of the function to get the symbols.")
	flag.StringVar(&packageName, "package", "", "The name of the package to get the symbols.")
	flag.BoolVar(&verbose, "verbose", false, "Print debug information.")
	flag.Parse()

	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedTypes |
			packages.NeedTypesInfo,
	}, packageName)

	if err != nil {
		log.Fatal(err)
	}
	if len(pkgs) == 0 {
		log.Fatal("No packages found")
	}

	pkg := pkgs[0]
	scope := pkg.Types.Scope()

	if verbose {
		fmt.Printf("Package: %s\n", pkg.Name)
		fmt.Printf("Go Files: %d\n", len(pkg.GoFiles))
		fmt.Printf("Compiled Files: %d\n", len(pkg.CompiledGoFiles))
		fmt.Printf("Errors: %d\n", len(pkg.Errors))

		for _, err := range pkg.Errors {
			fmt.Println("Load Error:", err)
		}

		if pkg.Types == nil {
			log.Fatal("pkg.Types is nil - types not loaded properly")
		}

		names := scope.Names()
		sort.Strings(names)

		fmt.Printf("\n=== All Names in Package Scope (%d total) ===\n", len(names))
		for _, name := range names {
			obj := scope.Lookup(name)
			fmt.Printf("%-30s : %T\n", name, obj)
		}
		//	fmt.Println("\n=== Target Variables NOT Found ===")
		//	fmt.Println("Possible reasons:")
		//	fmt.Println("1. Build constraints exclude the files containing these vars")
		//	fmt.Println("2. Variables are defined in a nested scope (inside a func)")
		//	fmt.Println("3. go:embed directive failed (missing .tpl files)")
		//	fmt.Println("4. Package wasn't built with full type info")
		fmt.Printf("\n=== Loaded Go Files ===\n")
		for i, f := range pkg.GoFiles {
			fmt.Printf("%d: %s\n", i+1, f)
		}

		buf.WriteString("\n=== Delve Debug Commands ===\n")
	}

	// Sanity check
	//	fmt.Printf("Loaded Package: %s (Path: %s)\n\n", pkg.Name, pkg.PkgPath)

	if pkg.Types == nil {
		log.Fatal("Types not loaded")
	}

	//	buf.WriteString("continue main.main\n")
	getSymbols(scope, pkg.PkgPath)
	buf.WriteTo(os.Stdout)
}
