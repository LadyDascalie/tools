// Copyright (c) Jeevanandam M (https://github.com/jeevatkm)
// go-aah/tools source code and usage is governed by a MIT style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"aahframework.org/essentials"
	"aahframework.org/log"
)

var buildImportCache map[string]string

type (
	// Program holds all details loaded from the Go source code for given Path.
	program struct {
		Path     string
		Packages []*packageInfo
	}

	// PackageInfo holds the single paackge information.
	packageInfo struct {
		Fset     *token.FileSet
		Pkg      *ast.Package
		Types    map[string]*typeInfo
		Path     string
		FilePath string
		Files    []string
	}

	// Type holds the information about type e.g. struct, func, custom type etc.
	typeInfo struct {
		Name          string
		Package       string
		EmbeddedTypes []*typeInfo
	}
)

//‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾
// Global methods
//___________________________________

// LoadProgram method loads the Go source code for the given directory.
func loadProgram(path string, excludes ess.Excludes) (*program, []error) {
	if err := validateInput(path); err != nil {
		return nil, append([]error{}, err)
	}

	prg := &program{
		Path:     path,
		Packages: []*packageInfo{},
	}

	var (
		pkgs map[string]*ast.Package
		errs []error
	)

	err := ess.Walk(path, func(srcPath string, info os.FileInfo, err error) error {
		if err != nil {
			errs = append(errs, err)
		}

		if excludes.Match(filepath.Base(srcPath)) {
			if info.IsDir() {
				// excluding directory
				return filepath.SkipDir
			}
			// excluding file
			return nil
		}

		if !info.IsDir() {
			return nil
		}

		if info.IsDir() && ess.IsDirEmpty(srcPath) {
			// skip directory if it's empty
			return filepath.SkipDir
		}

		pfset := token.NewFileSet()
		pkgs, err = parser.ParseDir(pfset, srcPath, func(f os.FileInfo) bool {
			return !f.IsDir() && !excludes.Match(f.Name())
		}, 0)

		if err != nil {
			if errList, ok := err.(scanner.ErrorList); ok {
				fmt.Println(errList)
			}

			errs = append(errs, fmt.Errorf("error parsing dir[%s]: %s", srcPath, err))
			return nil
		}

		pkg, err := validatePkgAndGet(pkgs, srcPath)
		if err != nil {
			errs = append(errs, err)
			return nil
		}

		if pkg != nil {
			pkg.FilePath = srcPath
			pkg.Path = stripGoPath(srcPath)
			prg.Packages = append(prg.Packages, pkg)
		}

		return nil
	})

	if err != nil {
		errs = append(errs, err)
	}

	return prg, errs
}

//‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾
// Program methods
//___________________________________

// FindPackage method returns the package info instance for given package name
// otherwise nil
func (prg *program) FindPackage(packageName string) *packageInfo {
	for _, p := range prg.Packages {
		if p.Name() == packageName {
			return p
		}
	}
	return nil
}

// Process method process particular packages in the program for `Type`,
// `Method`, etc.
func (prg *program) Process(packageName string) error {
	pkgInfo := prg.FindPackage(packageName)
	if pkgInfo == nil {
		return fmt.Errorf("package: %s not found", packageName)
	}

	pkgInfo.Types = map[string]*typeInfo{}

	// Each source file
	for name, file := range pkgInfo.Pkg.Files {
		pkgInfo.Files = append(pkgInfo.Files, filepath.Base(name))
		var fileImports *map[string]string

		// collecting imports
		for _, decl := range file.Decls {
			if genDecl, ok := decl.(*ast.GenDecl); ok {
				if isImportTok(genDecl) {
					fileImports = pkgInfo.processImports(genDecl)
				}
			}
		}

		// collecting types
		for _, decl := range file.Decls {
			if genDecl, ok := decl.(*ast.GenDecl); ok {
				if isTypeTok(genDecl) {
					pkgInfo.processTypes(genDecl, fileImports)
				}
			}
		}
	}

	return nil
}

//‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾
// PackageInfo methods
//___________________________________

// Name method return package name
func (p *packageInfo) Name() string {
	return p.Pkg.Name
}

func (p *packageInfo) processTypes(decl *ast.GenDecl, imports *map[string]string) {
	spec := decl.Specs[0].(*ast.TypeSpec)
	typeName := spec.Name.Name
	ty := &typeInfo{Name: typeName}

	// struct type
	st, ok := spec.Type.(*ast.StructType)
	if ok {
		// finding embedded type(s) and it's package
		for _, field := range st.Fields.List {
			// If field.Names is set, it's not an embedded type.
			if field.Names != nil && len(field.Names) > 0 {
				continue
			}

			fPkgName, fTypeName := findPkgAndTypeName(field.Type)
			_ = fPkgName // TODO need to work package import for embedded types

			// field type name empty, move on
			if ess.IsStrEmpty(fTypeName) {
				continue
			}
		}

	}

	p.Types[strings.ToLower(typeName)] = ty
}

func (p *packageInfo) processImports(decl *ast.GenDecl) *map[string]string {
	imports := map[string]string{}
	for _, dspec := range decl.Specs {
		spec := dspec.(*ast.ImportSpec)
		var pkgAlias string
		if spec.Name != nil {
			if spec.Name.Name == "_" {
				continue
			}

			pkgAlias = spec.Name.Name
		}

		importPath := spec.Path.Value[1 : len(spec.Path.Value)-1]
		if ess.IsStrEmpty(pkgAlias) {
			if alias, found := buildImportCache[importPath]; found {
				pkgAlias = alias
			} else { // build cache
				pkg, err := build.Import(importPath, p.FilePath, 0)
				if err != nil {
					log.Errorf("Unable to find import path: %s", importPath)
					continue
				}
				pkgAlias = pkg.Name
				buildImportCache[importPath] = pkg.Name
			}
		}

		imports[pkgAlias] = importPath
	}

	return &imports
}

//‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾
// Unexported methods
//___________________________________

func validateInput(path string) error {
	if ess.IsStrEmpty(path) {
		return errors.New("path is required input")
	}

	if !ess.IsFileExists(path) {
		return fmt.Errorf("path is does not exists: %s", path)
	}

	return nil
}

func validatePkgAndGet(pkgs map[string]*ast.Package, path string) (*packageInfo, error) {
	pkgCnt := len(pkgs)

	// no source code found in the directory
	if pkgCnt == 0 {
		return nil, nil
	}

	// not permitted by Go lang spec
	if pkgCnt > 1 {
		var names []string
		for k := range pkgs {
			names = append(names, k)
		}
		return nil, fmt.Errorf("more than one package name [%s] found in single"+
			" directory: %s", strings.Join(names, ", "), path)
	}

	pkg := &packageInfo{}
	for _, v := range pkgs {
		pkg.Pkg = v
	}

	return pkg, nil
}

func isImportTok(decl *ast.GenDecl) bool {
	return token.IMPORT == decl.Tok
}

func isTypeTok(decl *ast.GenDecl) bool {
	return token.TYPE == decl.Tok
}

func stripGoPath(pkgFilePath string) string {
	idx := strings.Index(pkgFilePath, "src")
	return filepath.Clean(pkgFilePath[idx+4:])
}

// findPkgAndTypeName method to find a direct "embedded|sub-type".
// It has an ast.Field as follows:
//   Ident { "type-name" } e.g. UserController
//   SelectorExpr { "package-name", "type-name" } e.g. aah.Controller
// Additionally, that can be wrapped by StarExprs.
func findPkgAndTypeName(fieldType ast.Expr) (string, string) {
	for {
		if starExpr, ok := fieldType.(*ast.StarExpr); ok {
			fieldType = starExpr.X
			continue
		}
		break
	}

	// Embedded type it's in the same package, it's an ast.Ident.
	if ident, ok := fieldType.(*ast.Ident); ok {
		return "", ident.Name
	}

	// Embedded type it's in the different package, it's an ast.SelectorExpr.
	if selectorExpr, ok := fieldType.(*ast.SelectorExpr); ok {
		if pkgIdent, ok := selectorExpr.X.(*ast.Ident); ok {
			return pkgIdent.Name, selectorExpr.Sel.Name
		}
	}

	return "", ""
}

func init() {
	buildImportCache = map[string]string{}
}
