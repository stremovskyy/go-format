package advice

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"

	"github.com/stremovskyy/go-format/internal/readability"
)

func Analyze(path string, src []byte) ([]readability.Issue, error) {
	if readability.IsGenerated(src) {
		return nil, nil
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var issues []readability.Issue
	add := func(pos token.Pos, rule string, message string) {
		issues = append(issues, readability.Issue{
			File:    path,
			Line:    fset.Position(pos).Line,
			Rule:    rule,
			Message: message,
			Fixable: false,
		})
	}

	checkTODOComments(file, add)
	checkExportedDocs(file, add)
	checkTopLevelStructPadding(file, add)
	checkReceiverNames(file, add)
	checkFunctions(file, add)

	sortIssues(issues)

	return issues, nil
}

func checkTODOComments(file *ast.File, add func(token.Pos, string, string)) {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(comment.Text, "//"), "/*"))

			if !strings.Contains(text, "TODO") {
				continue
			}

			idx := strings.Index(text, "TODO")
			todo := text[idx:]

			if strings.HasPrefix(todo, "TODO(") && strings.Contains(todo, "): ") {
				continue
			}

			add(comment.Pos(), "todo-format", "TODO should use format TODO(owner): text")
		}
	}
}

func checkExportedDocs(file *ast.File, add func(token.Pos, string, string)) {
	for _, decl := range file.Decls {
		switch current := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range current.Specs {
				switch typed := spec.(type) {
				case *ast.TypeSpec:
					if ast.IsExported(typed.Name.Name) && !docStartsWith(current.Doc, typed.Name.Name) {
						add(typed.Pos(), "exported-doc", "exported type "+typed.Name.Name+" should have a doc comment")
					}

				case *ast.ValueSpec:
					for _, name := range typed.Names {
						if ast.IsExported(name.Name) && !docStartsWith(current.Doc, name.Name) {
							add(name.Pos(), "exported-doc", "exported value "+name.Name+" should have a doc comment")
						}
					}
				}
			}

		case *ast.FuncDecl:
			if current.Recv == nil && ast.IsExported(current.Name.Name) && !docStartsWith(current.Doc, current.Name.Name) {
				add(current.Pos(), "exported-doc", "exported function "+current.Name.Name+" should have a doc comment")
			}
		}
	}
}

func docStartsWith(doc *ast.CommentGroup, name string) bool {
	if doc == nil {
		return false
	}

	return strings.HasPrefix(strings.TrimSpace(doc.Text()), name)
}

func checkTopLevelStructPadding(file *ast.File, add func(token.Pos, string, string)) {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)

		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)

			if !ok {
				continue
			}

			structType, ok := typeSpec.Type.(*ast.StructType)

			if !ok || structType.Fields == nil {
				continue
			}

			checkStructPadding(typeSpec.Name.Name, structType, add)
		}
	}
}

func checkStructPadding(name string, structType *ast.StructType, add func(token.Pos, string, string)) {
	var previous *ast.Field
	previousSize := 0
	previousName := ""

	for _, field := range structType.Fields.List {
		size := approximateFieldSize(field.Type)
		fieldName := firstFieldName(field)

		if previous != nil && previousSize > 0 && size > previousSize && previousSize < 8 {
			add(
				field.Pos(),
				"struct-padding",
				"struct "+name+" may waste memory: field "+fieldName+" follows smaller field "+previousName,
			)

			return
		}

		previous = field
		previousSize = size
		previousName = fieldName
	}
}

func approximateFieldSize(expr ast.Expr) int {
	switch current := expr.(type) {
	case *ast.Ident:
		switch current.Name {
		case "bool", "byte", "int8", "uint8":
			return 1
		case "int16", "uint16":
			return 2
		case "int32", "uint32", "rune", "float32":
			return 4
		case "int", "uint", "uintptr", "int64", "uint64", "float64", "complex64":
			return 8
		case "string", "error", "any":
			return 16
		default:
			return 8
		}

	case *ast.ArrayType:
		if current.Len == nil {
			return 24
		}

		return 8

	case *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.StarExpr:
		return 8
	case *ast.InterfaceType:
		return 16
	case *ast.SelectorExpr:
		return 8
	default:
		return 8
	}
}

func firstFieldName(field *ast.Field) string {
	if len(field.Names) > 0 {
		return field.Names[0].Name
	}

	return exprName(field.Type)
}

func checkReceiverNames(file *ast.File, add func(token.Pos, string, string)) {
	seen := map[string]string{}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)

		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 || len(fn.Recv.List[0].Names) == 0 {
			continue
		}

		receiverType := exprName(fn.Recv.List[0].Type)
		receiverName := fn.Recv.List[0].Names[0].Name

		if receiverType == "" || receiverName == "_" {
			continue
		}

		if first, ok := seen[receiverType]; ok && first != receiverName {
			add(
				fn.Pos(),
				"receiver-name",
				"receiver name for "+receiverType+" is inconsistent: saw "+first+" and "+receiverName,
			)

			continue
		}

		seen[receiverType] = receiverName
	}
}

func checkFunctions(file *ast.File, add func(token.Pos, string, string)) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)

		if !ok || fn.Body == nil {
			continue
		}

		checkContextFirst(fn, add)
		checkFunctionCalls(fn, add)
		checkDefersInLoops(fn, add)
		checkAppendsInLoops(fn, add)
		checkBuilderGrow(fn, add)
	}
}

func checkContextFirst(fn *ast.FuncDecl, add func(token.Pos, string, string)) {
	if fn.Type.Params == nil || len(fn.Type.Params.List) < 2 {
		return
	}

	for idx, param := range fn.Type.Params.List {
		if isContextType(param.Type) && idx > 0 {
			add(fn.Pos(), "context-first", "context.Context should be the first parameter")

			return
		}
	}
}

func checkFunctionCalls(fn *ast.FuncDecl, add func(token.Pos, string, string)) {
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)

		if !ok {
			return true
		}

		if selectorName(call.Fun) == "fmt.Errorf" && fmtErrorUsesPercentV(call) {
			add(call.Pos(), "error-wrap", "fmt.Errorf should wrap err with %w instead of %v")
		}

		if selectorName(call.Fun) == "regexp.MustCompile" {
			add(call.Pos(), "regexp-mustcompile", "regexp.MustCompile inside function should be hoisted to package scope")
		}

		return true
	})
}

func fmtErrorUsesPercentV(call *ast.CallExpr) bool {
	if len(call.Args) < 2 {
		return false
	}

	format, ok := stringLiteral(call.Args[0])

	if !ok || !strings.Contains(format, "%v") || strings.Contains(format, "%w") {
		return false
	}

	for _, arg := range call.Args[1:] {
		if ident, ok := arg.(*ast.Ident); ok && ident.Name == "err" {
			return true
		}
	}

	return false
}

func checkDefersInLoops(fn *ast.FuncDecl, add func(token.Pos, string, string)) {
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch loop := node.(type) {
		case *ast.ForStmt:
			reportLoopDefers(loop.Body, add)
		case *ast.RangeStmt:
			reportLoopDefers(loop.Body, add)
		}

		return true
	})
}

func reportLoopDefers(body *ast.BlockStmt, add func(token.Pos, string, string)) {
	ast.Inspect(body, func(node ast.Node) bool {
		deferStmt, ok := node.(*ast.DeferStmt)

		if !ok {
			return true
		}

		add(deferStmt.Pos(), "defer-in-loop", "defer inside loop may delay cleanup until function return")

		return true
	})
}

func checkAppendsInLoops(fn *ast.FuncDecl, add func(token.Pos, string, string)) {
	preallocated := preallocatedSlices(fn.Body)

	ast.Inspect(fn.Body, func(node ast.Node) bool {
		var body *ast.BlockStmt

		switch loop := node.(type) {
		case *ast.ForStmt:
			body = loop.Body
		case *ast.RangeStmt:
			body = loop.Body
		default:
			return true
		}

		ast.Inspect(body, func(loopNode ast.Node) bool {
			assign, ok := loopNode.(*ast.AssignStmt)

			if !ok || len(assign.Lhs) == 0 || len(assign.Rhs) == 0 {
				return true
			}

			target, ok := assign.Lhs[0].(*ast.Ident)

			if !ok || preallocated[target.Name] {
				return true
			}

			call, ok := assign.Rhs[0].(*ast.CallExpr)

			if !ok || identName(call.Fun) != "append" || len(call.Args) == 0 {
				return true
			}

			appended, ok := call.Args[0].(*ast.Ident)

			if !ok || appended.Name != target.Name {
				return true
			}

			add(assign.Pos(), "append-prealloc", "append in loop to "+target.Name+" may need preallocation")

			return true
		})

		return true
	})
}

func preallocatedSlices(body *ast.BlockStmt) map[string]bool {
	preallocated := map[string]bool{}

	ast.Inspect(body, func(node ast.Node) bool {
		switch current := node.(type) {
		case *ast.AssignStmt:
			for idx, lhs := range current.Lhs {
				name := identName(lhs)

				if name == "" || idx >= len(current.Rhs) {
					continue
				}

				if makeHasCapacity(current.Rhs[idx]) {
					preallocated[name] = true
				}
			}

		case *ast.ValueSpec:
			for idx, name := range current.Names {
				if idx >= len(current.Values) {
					continue
				}

				if makeHasCapacity(current.Values[idx]) {
					preallocated[name.Name] = true
				}
			}
		}

		return true
	})

	return preallocated
}

func makeHasCapacity(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)

	if !ok || identName(call.Fun) != "make" {
		return false
	}

	if len(call.Args) >= 3 {
		return true
	}

	if len(call.Args) == 2 {
		if literal, ok := call.Args[1].(*ast.BasicLit); ok && literal.Value == "0" {
			return false
		}

		return true
	}

	return false
}

func checkBuilderGrow(fn *ast.FuncDecl, add func(token.Pos, string, string)) {
	builders := map[string]struct{}{}
	hasGrow := map[string]bool{}
	firstLiteralWrite := map[string]token.Pos{}

	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch current := node.(type) {
		case *ast.DeclStmt:
			genDecl, ok := current.Decl.(*ast.GenDecl)

			if !ok {
				return true
			}

			for _, spec := range genDecl.Specs {
				values, ok := spec.(*ast.ValueSpec)

				if !ok || selectorName(values.Type) != "strings.Builder" {
					continue
				}

				for _, name := range values.Names {
					builders[name.Name] = struct{}{}
				}
			}

		case *ast.AssignStmt:
			for idx, lhs := range current.Lhs {
				name := identName(lhs)

				if name == "" || idx >= len(current.Rhs) {
					continue
				}

				if compositeTypeName(current.Rhs[idx]) == "strings.Builder" {
					builders[name] = struct{}{}
				}
			}

		case *ast.CallExpr:
			receiver, method, ok := selectorParts(current.Fun)

			if !ok {
				return true
			}

			if method == "Grow" {
				hasGrow[receiver] = true
			}

			if method == "WriteString" && len(current.Args) == 1 {
				if _, ok := builders[receiver]; !ok {
					return true
				}

				if _, ok := stringLiteral(current.Args[0]); ok {
					if _, exists := firstLiteralWrite[receiver]; !exists {
						firstLiteralWrite[receiver] = current.Pos()
					}
				}
			}
		}

		return true
	})

	for builder, pos := range firstLiteralWrite {
		if hasGrow[builder] {
			continue
		}

		add(pos, "builder-grow", "strings.Builder "+builder+" writes string literals without Grow")
	}
}

func isContextType(expr ast.Expr) bool {
	return selectorName(expr) == "context.Context"
}

func selectorName(expr ast.Expr) string {
	receiver, name, ok := selectorParts(expr)

	if !ok {
		return ""
	}

	return receiver + "." + name
}

func selectorParts(expr ast.Expr) (string, string, bool) {
	selector, ok := expr.(*ast.SelectorExpr)

	if !ok {
		return "", "", false
	}

	return exprName(selector.X), selector.Sel.Name, true
}

func identName(expr ast.Expr) string {
	ident, ok := expr.(*ast.Ident)

	if !ok {
		return ""
	}

	return ident.Name
}

func exprName(expr ast.Expr) string {
	switch current := expr.(type) {
	case *ast.Ident:
		return current.Name
	case *ast.StarExpr:
		return exprName(current.X)

	case *ast.SelectorExpr:
		prefix := exprName(current.X)

		if prefix == "" {
			return current.Sel.Name
		}

		return prefix + "." + current.Sel.Name

	case *ast.IndexExpr:
		return exprName(current.X)
	case *ast.IndexListExpr:
		return exprName(current.X)
	default:
		return ""
	}
}

func compositeTypeName(expr ast.Expr) string {
	composite, ok := expr.(*ast.CompositeLit)

	if !ok {
		return ""
	}

	return exprName(composite.Type)
}

func stringLiteral(expr ast.Expr) (string, bool) {
	literal, ok := expr.(*ast.BasicLit)

	if !ok || literal.Kind != token.STRING {
		return "", false
	}

	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}

	return value, true
}

func sortIssues(issues []readability.Issue) {
	for idx := 1; idx < len(issues); idx++ {
		current := issues[idx]
		prev := idx - 1

		for prev >= 0 && issueLess(current, issues[prev]) {
			issues[prev+1] = issues[prev]
			prev--
		}

		issues[prev+1] = current
	}
}

func issueLess(left readability.Issue, right readability.Issue) bool {
	if left.File != right.File {
		return left.File < right.File
	}

	if left.Line != right.Line {
		return left.Line < right.Line
	}

	return left.Rule < right.Rule
}
