package mutate

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"sort"
	"strconv"
	"strings"

	"github.com/stremovskyy/go-format/internal/readability"
)

type edit struct {
	start   int
	end     int
	text    string
	pos     token.Pos
	rule    string
	message string
}

func Rewrite(path string, src []byte) ([]byte, []readability.Issue, error) {
	if readability.IsGenerated(src) {
		return src, nil, nil
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var edits []edit
	add := func(pos token.Pos, end token.Pos, text string, rule string, message string) {
		edits = append(edits, edit{
			start:   fset.Position(pos).Offset,
			end:     fset.Position(end).Offset,
			text:    text,
			pos:     pos,
			rule:    rule,
			message: message,
		})
	}
	insert := func(pos token.Pos, text string, rule string, message string) {
		add(pos, pos, text, rule, message)
	}

	addDocStubEdits(fset, src, file, insert)
	addErrorWrapEdits(file, add)
	addBoolReturnEdits(fset, file, add)

	if len(edits) == 0 {
		return src, nil, nil
	}

	sort.Slice(edits, func(i, j int) bool {
		if edits[i].start != edits[j].start {
			return edits[i].start < edits[j].start
		}

		return edits[i].end < edits[j].end
	})

	for idx := 1; idx < len(edits); idx++ {
		if edits[idx].start < edits[idx-1].end {
			return nil, nil, fmt.Errorf("mutate %s: overlapping edits for %s and %s", path, edits[idx-1].rule, edits[idx].rule)
		}
	}

	out := append([]byte(nil), src...)
	for idx := len(edits) - 1; idx >= 0; idx-- {
		current := edits[idx]
		out = append(out[:current.start], append([]byte(current.text), out[current.end:]...)...)
	}

	issues := make([]readability.Issue, 0, len(edits))
	for _, current := range edits {
		issues = append(issues, readability.Issue{
			File:    path,
			Line:    fset.Position(current.pos).Line,
			Rule:    current.rule,
			Message: current.message,
			Fixable: true,
		})
	}

	return out, issues, nil
}

func addDocStubEdits(
	fset *token.FileSet,
	src []byte,
	file *ast.File,
	insert func(token.Pos, string, string, string),
) {
	for _, decl := range file.Decls {
		switch current := decl.(type) {
		case *ast.GenDecl:
			addGenDeclDocStub(fset, src, current, insert)
		case *ast.FuncDecl:
			if current.Recv == nil && ast.IsExported(current.Name.Name) && !docStartsWith(current.Doc, current.Name.Name) {
				insert(
					current.Pos(),
					docStubText(current.Name.Name, ""),
					"doc-stub",
					"generated GoDoc stub for exported function "+current.Name.Name,
				)
			}
		}
	}
}

func addGenDeclDocStub(
	fset *token.FileSet,
	src []byte,
	decl *ast.GenDecl,
	insert func(token.Pos, string, string, string),
) {
	if decl.Tok != token.TYPE && decl.Tok != token.CONST && decl.Tok != token.VAR {
		return
	}

	for _, spec := range decl.Specs {
		switch current := spec.(type) {
		case *ast.TypeSpec:
			if !ast.IsExported(current.Name.Name) || docStartsWith(specDoc(current), current.Name.Name, decl.Doc) {
				continue
			}

			pos := docInsertPos(decl, current.Pos())
			indent := indentBeforeOffset(src, fset.Position(pos).Offset)
			insert(
				pos,
				docStubText(current.Name.Name, indent),
				"doc-stub",
				"generated GoDoc stub for exported type "+current.Name.Name,
			)

		case *ast.ValueSpec:
			name := singleExportedName(current.Names)
			if name == "" || docStartsWith(specDoc(current), name, decl.Doc) {
				continue
			}

			pos := docInsertPos(decl, current.Pos())
			indent := indentBeforeOffset(src, fset.Position(pos).Offset)
			insert(
				pos,
				docStubText(name, indent),
				"doc-stub",
				"generated GoDoc stub for exported value "+name,
			)
		}
	}
}

func docInsertPos(decl *ast.GenDecl, specPos token.Pos) token.Pos {
	if decl.Lparen == token.NoPos {
		return decl.Pos()
	}

	return specPos
}

func specDoc(spec ast.Spec) *ast.CommentGroup {
	switch current := spec.(type) {
	case *ast.TypeSpec:
		return current.Doc
	case *ast.ValueSpec:
		return current.Doc
	default:
		return nil
	}
}

func singleExportedName(names []*ast.Ident) string {
	exported := ""

	for _, name := range names {
		if !ast.IsExported(name.Name) {
			continue
		}

		if exported != "" {
			return ""
		}

		exported = name.Name
	}

	return exported
}

func docStartsWithName(doc *ast.CommentGroup, name string) bool {
	if doc == nil {
		return false
	}

	return strings.HasPrefix(strings.TrimSpace(doc.Text()), name)
}

func docStartsWith(doc *ast.CommentGroup, name string, inherited ...*ast.CommentGroup) bool {
	if docStartsWithName(doc, name) {
		return true
	}

	for _, candidate := range inherited {
		if docStartsWithName(candidate, name) {
			return true
		}
	}

	return false
}

func docStubText(name string, indent string) string {
	return "// " + name + " ...\n" + indent
}

func indentBeforeOffset(src []byte, offset int) string {
	lineStart := offset
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}

	return string(src[lineStart:offset])
}

func addErrorWrapEdits(file *ast.File, add func(token.Pos, token.Pos, string, string, string)) {
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !isFmtErrorf(call) || len(call.Args) != 2 {
			return true
		}

		formatLit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || formatLit.Kind != token.STRING {
			return true
		}

		errArg, ok := call.Args[1].(*ast.Ident)
		if !ok || errArg.Name != "err" {
			return true
		}

		format, err := strconv.Unquote(formatLit.Value)
		if err != nil || hasSimpleVerb(format, 'w') {
			return true
		}

		idx := simpleVerbIndex(format, 'v')
		if idx < 0 {
			return true
		}

		replacement := format[:idx+1] + "w" + format[idx+2:]
		add(
			formatLit.Pos(),
			formatLit.End(),
			strconv.Quote(replacement),
			"error-wrap-fix",
			"rewrote fmt.Errorf to wrap err with %w",
		)

		return true
	})
}

func isFmtErrorf(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Errorf" {
		return false
	}

	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "fmt"
}

func hasSimpleVerb(format string, verb byte) bool {
	return simpleVerbIndex(format, verb) >= 0
}

func simpleVerbIndex(format string, verb byte) int {
	for idx := 0; idx < len(format)-1; idx++ {
		if format[idx] != '%' {
			continue
		}

		next := format[idx+1]
		if next == '%' {
			idx++

			continue
		}

		if next == verb {
			return idx
		}
	}

	return -1
}

func addBoolReturnEdits(fset *token.FileSet, file *ast.File, add func(token.Pos, token.Pos, string, string, string)) {
	ast.Inspect(file, func(node ast.Node) bool {
		block, ok := node.(*ast.BlockStmt)
		if !ok {
			return true
		}

		for idx := 0; idx < len(block.List)-1; idx++ {
			ifStmt, ok := block.List[idx].(*ast.IfStmt)
			if !ok || ifStmt.Init != nil || ifStmt.Else != nil || len(ifStmt.Body.List) != 1 {
				continue
			}

			firstValue, ok := boolReturnValue(ifStmt.Body.List[0])
			if !ok {
				continue
			}

			secondValue, ok := boolReturnValue(block.List[idx+1])
			if !ok || firstValue == secondValue || hasCommentBetween(file.Comments, ifStmt.Pos(), block.List[idx+1].End()) {
				continue
			}

			expr, err := renderExpr(fset, ifStmt.Cond)
			if err != nil {
				continue
			}

			replacement := "return " + expr
			if !firstValue && secondValue {
				replacement = "return " + negateExpr(expr, ifStmt.Cond)
			}

			add(
				ifStmt.Pos(),
				block.List[idx+1].End(),
				replacement,
				"bool-return-simplify",
				"simplified if/return bool chain",
			)
		}

		return true
	})
}

func boolReturnValue(stmt ast.Stmt) (bool, bool) {
	ret, ok := stmt.(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false, false
	}

	ident, ok := ret.Results[0].(*ast.Ident)
	if !ok {
		return false, false
	}

	switch ident.Name {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func renderExpr(fset *token.FileSet, expr ast.Expr) (string, error) {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, expr); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func negateExpr(rendered string, expr ast.Expr) string {
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.NOT {
		return strings.TrimPrefix(rendered, "!")
	}

	switch expr.(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.CallExpr, *ast.IndexExpr, *ast.IndexListExpr:
		return "!" + rendered
	default:
		return "!(" + rendered + ")"
	}
}

func hasCommentBetween(comments []*ast.CommentGroup, start token.Pos, end token.Pos) bool {
	for _, group := range comments {
		if group.Pos() > start && group.End() < end {
			return true
		}
	}

	return false
}
