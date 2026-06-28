package readability

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type Issue struct {
	File    string
	Line    int
	Rule    string
	Message string
	Fixable bool
}

type insertion struct {
	rule    string
	message string
}

func Rewrite(path string, src []byte) ([]byte, []Issue, error) {
	if IsGenerated(src) {
		return src, nil, nil
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	lines := splitLines(src)
	comments := commentSpans(fset, file)
	insertions := map[int]insertion{}

	checkTopLevelDecls(fset, lines, comments, file.Decls, insertions)
	checkTypeFieldGroups(fset, lines, comments, file.Decls, insertions)

	ast.Inspect(file, func(node ast.Node) bool {
		switch current := node.(type) {
		case *ast.BlockStmt:
			checkStmtList(fset, lines, comments, current.List, insertions)
		case *ast.CaseClause:
			checkStmtList(fset, lines, comments, current.Body, insertions)
		case *ast.CommClause:
			checkStmtList(fset, lines, comments, current.Body, insertions)
		case *ast.SwitchStmt:
			checkSwitchClauses(fset, lines, current.Body.List, insertions)
		case *ast.TypeSwitchStmt:
			checkSwitchClauses(fset, lines, current.Body.List, insertions)
		}

		return true
	})

	issues := make([]Issue, 0, len(insertions))

	for line, item := range insertions {
		issues = append(issues, Issue{
			File:    path,
			Line:    line,
			Rule:    item.rule,
			Message: item.message,
			Fixable: true,
		})
	}

	sort.Slice(issues, func(i, j int) bool {
		if issues[i].File != issues[j].File {
			return issues[i].File < issues[j].File
		}

		return issues[i].Line < issues[j].Line
	})

	if len(insertions) == 0 {
		return src, nil, nil
	}

	return applyInsertions(src, insertions), issues, nil
}

func RewriteFile(path string) ([]Issue, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	rewritten, issues, err := Rewrite(path, src)
	if err != nil {
		return nil, err
	}

	if !bytes.Equal(src, rewritten) {
		if err := os.WriteFile(path, rewritten, 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
	}

	return issues, nil
}

func CollectFiles(paths []string, includeHidden bool, exclude []string) ([]string, error) {
	if len(paths) == 0 {
		paths = []string{"."}
	}

	var files []string
	seen := map[string]struct{}{}
	addFile := func(path string) {
		cleanPath := filepath.Clean(path)
		key, err := filepath.Abs(cleanPath)
		if err != nil {
			key = cleanPath
		}

		if _, ok := seen[key]; ok {
			return
		}

		seen[key] = struct{}{}
		files = append(files, cleanPath)
	}

	for _, path := range paths {
		path = normalizeRecursivePath(path)

		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", path, err)
		}

		if !info.IsDir() {
			base := filepath.Dir(path)

			if strings.HasSuffix(path, ".go") && !isGeneratedPath(path) && !isExcludedPath(base, path, exclude) {
				addFile(path)
			}

			continue
		}

		root := path

		err = filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if current != path && entry.IsDir() && skipDir(entry.Name(), includeHidden) {
				return filepath.SkipDir
			}

			if current != path && isExcludedPath(root, current, exclude) {
				if entry.IsDir() {
					return filepath.SkipDir
				}

				return nil
			}

			if !entry.IsDir() && strings.HasSuffix(current, ".go") && !isGeneratedPath(current) {
				addFile(current)
			}

			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Strings(files)

	return files, nil
}

func isExcludedPath(root string, candidate string, exclude []string) bool {
	if len(exclude) == 0 {
		return false
	}

	rel, err := filepath.Rel(root, candidate)

	if err != nil || rel == "." {
		rel = candidate
	}

	rel = filepath.ToSlash(filepath.Clean(rel))
	base := filepath.Base(candidate)

	for _, pattern := range exclude {
		pattern = filepath.ToSlash(filepath.Clean(strings.TrimSpace(pattern)))

		if pattern == "" || pattern == "." {
			continue
		}

		if matchExcludePattern(pattern, rel, base) {
			return true
		}
	}

	return false
}

func matchExcludePattern(pattern string, rel string, base string) bool {
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")

		return rel == prefix || strings.HasPrefix(rel, prefix+"/")
	}

	if matched, _ := path.Match(pattern, rel); matched {
		return true
	}

	if !strings.Contains(pattern, "/") {
		if matched, _ := path.Match(pattern, base); matched {
			return true
		}
	}

	return false
}

func normalizeRecursivePath(path string) string {
	if path == "..." {
		return "."
	}

	if strings.HasSuffix(path, string(filepath.Separator)+"...") {
		base := strings.TrimSuffix(path, string(filepath.Separator)+"...")

		if base == "" {
			return string(filepath.Separator)
		}

		return base
	}

	if strings.HasSuffix(path, "/...") {
		base := strings.TrimSuffix(path, "/...")

		if base == "" {
			return "."
		}

		return base
	}

	return path
}

func IsGenerated(src []byte) bool {
	lines := splitLines(src)

	return generatedHeader(lines)
}

func isGeneratedPath(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}

	defer file.Close()

	lines := make([]string, 0, 20)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())

		if len(lines) >= 20 {
			break
		}
	}

	return generatedHeader(lines)
}

func generatedHeader(lines []string) bool {
	header := strings.Join(lines[:min(20, len(lines))], "\n")

	return strings.Contains(header, "Code generated") && strings.Contains(header, "DO NOT EDIT")
}

func skipDir(name string, includeHidden bool) bool {
	switch name {
	case ".git", "node_modules", "third_party", "vendor":
		return true
	}

	return !includeHidden && strings.HasPrefix(name, ".")
}

func splitLines(src []byte) []string {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	lines := strings.Split(text, "\n")

	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

type commentSpan struct {
	start int
	end   int
}

func commentSpans(fset *token.FileSet, file *ast.File) []commentSpan {
	comments := make([]commentSpan, 0, len(file.Comments))

	for _, group := range file.Comments {
		comments = append(comments, commentSpan{
			start: fset.Position(group.Pos()).Line,
			end:   fset.Position(group.End()).Line,
		})
	}

	sort.Slice(comments, func(i, j int) bool {
		if comments[i].start != comments[j].start {
			return comments[i].start < comments[j].start
		}

		return comments[i].end < comments[j].end
	})

	return comments
}

func checkTopLevelDecls(
	fset *token.FileSet,
	lines []string,
	comments []commentSpan,
	decls []ast.Decl,
	insertions map[int]insertion,
) {
	for idx := 1; idx < len(decls); idx++ {
		prev := decls[idx-1]
		curr := decls[idx]

		rule, message, ok := blankBeforeDecl(prev, curr)

		if !ok {
			continue
		}

		prevEnd := fset.Position(prev.End()).Line
		currStart := fset.Position(curr.Pos()).Line
		line := insertionLine(prevEnd, currStart, comments)

		if hasBlankBetweenLines(lines, prevEnd, line) {
			continue
		}

		addInsertion(insertions, line, rule, message)
	}
}

func blankBeforeDecl(prev ast.Decl, curr ast.Decl) (string, string, bool) {
	prevKind := declGroupKind(prev)
	currKind := declGroupKind(curr)

	if prevKind == "" || currKind == "" {
		return "", "", false
	}

	if prevKind != currKind {
		return "decl-spacing", "missing blank line before top-level declaration group", true
	}

	prevReceiver := receiverTypeName(prev)
	currReceiver := receiverTypeName(curr)

	if prevReceiver != "" && currReceiver != "" && prevReceiver != currReceiver {
		return "receiver-spacing", "missing blank line before methods for different receiver type", true
	}

	return "", "", false
}

func declGroupKind(decl ast.Decl) string {
	switch current := decl.(type) {
	case *ast.GenDecl:
		return current.Tok.String()
	case *ast.FuncDecl:
		return "func"
	default:
		return ""
	}
}

func receiverTypeName(decl ast.Decl) string {
	current, ok := decl.(*ast.FuncDecl)

	if !ok || current.Recv == nil || len(current.Recv.List) == 0 {
		return ""
	}

	return exprNameForGrouping(current.Recv.List[0].Type)
}

func exprNameForGrouping(expr ast.Expr) string {
	switch current := expr.(type) {
	case *ast.Ident:
		return current.Name
	case *ast.StarExpr:
		return exprNameForGrouping(current.X)
	case *ast.IndexExpr:
		return exprNameForGrouping(current.X)
	case *ast.IndexListExpr:
		return exprNameForGrouping(current.X)

	case *ast.SelectorExpr:
		prefix := exprNameForGrouping(current.X)

		if prefix == "" {
			return current.Sel.Name
		}

		return prefix + "." + current.Sel.Name

	default:
		return ""
	}
}

func checkTypeFieldGroups(
	fset *token.FileSet,
	lines []string,
	comments []commentSpan,
	decls []ast.Decl,
	insertions map[int]insertion,
) {
	for _, decl := range decls {
		genDecl, ok := decl.(*ast.GenDecl)

		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)

			if !ok {
				continue
			}

			switch current := typeSpec.Type.(type) {
			case *ast.StructType:
				checkFieldListGroups(
					fset,
					lines,
					comments,
					current.Fields.List,
					structFieldCategory,
					"struct-field-groups",
					"missing blank line before struct field group",
					insertions,
				)

			case *ast.InterfaceType:
				checkFieldListGroups(
					fset,
					lines,
					comments,
					current.Methods.List,
					interfaceFieldCategory,
					"interface-field-groups",
					"missing blank line before interface member group",
					insertions,
				)
			}
		}
	}
}

func checkFieldListGroups(
	fset *token.FileSet,
	lines []string,
	comments []commentSpan,
	fields []*ast.Field,
	category func(*ast.Field) string,
	rule string,
	message string,
	insertions map[int]insertion,
) {
	for idx := 1; idx < len(fields); idx++ {
		prev := fields[idx-1]
		curr := fields[idx]

		if category(prev) == category(curr) {
			continue
		}

		prevEnd := fset.Position(prev.End()).Line
		currStart := fset.Position(curr.Pos()).Line
		line := insertionLine(prevEnd, currStart, comments)

		if hasBlankBetweenLines(lines, prevEnd, line) {
			continue
		}

		addInsertion(insertions, line, rule, message)
	}
}

func structFieldCategory(field *ast.Field) string {
	if len(field.Names) == 0 {
		return "embedded"
	}

	if _, ok := field.Type.(*ast.FuncType); ok {
		return "callback"
	}

	return "field"
}

func interfaceFieldCategory(field *ast.Field) string {
	if len(field.Names) == 0 {
		return "embedded"
	}

	return "method"
}

func checkStmtList(
	fset *token.FileSet,
	lines []string,
	comments []commentSpan,
	stmts []ast.Stmt,
	insertions map[int]insertion,
) {
	for idx := 1; idx < len(stmts); idx++ {
		prev := stmts[idx-1]
		curr := stmts[idx]

		if rule, message, ok := blankBeforeStmt(prev, curr, idx == len(stmts)-1); ok {
			prevEnd := fset.Position(prev.End()).Line
			currStart := fset.Position(curr.Pos()).Line
			line := insertionLine(prevEnd, currStart, comments)

			if hasBlankBetweenLines(lines, prevEnd, line) {
				continue
			}

			addInsertion(insertions, line, rule, message)
		}
	}
}

func checkSwitchClauses(
	fset *token.FileSet,
	lines []string,
	clauses []ast.Stmt,
	insertions map[int]insertion,
) {
	for idx := 1; idx < len(clauses); idx++ {
		prev, prevOK := clauses[idx-1].(*ast.CaseClause)
		curr, currOK := clauses[idx].(*ast.CaseClause)

		if !prevOK || !currOK || (!caseClauseIsLarge(fset, prev) && !caseClauseIsLarge(fset, curr)) {
			continue
		}

		prevEnd := fset.Position(prev.End()).Line
		currStart := fset.Position(curr.Pos()).Line

		if hasBlankBetweenLines(lines, prevEnd, currStart) {
			continue
		}

		addInsertion(
			insertions,
			currStart,
			"switch-case-spacing",
			"missing blank line before large switch case group",
		)
	}
}

func caseClauseIsLarge(fset *token.FileSet, clause *ast.CaseClause) bool {
	if len(clause.Body) > 1 {
		return true
	}

	if len(clause.Body) == 1 {
		start := fset.Position(clause.Body[0].Pos()).Line
		end := fset.Position(clause.Body[0].End()).Line

		return end > start
	}

	return false
}

func blankBeforeStmt(prev ast.Stmt, curr ast.Stmt, isLast bool) (string, string, bool) {
	if isCoupledErrorCheck(prev, curr) {
		return "", "", false
	}

	if isStandaloneControl(prev) {
		return "logical-block-spacing", "missing blank line after standalone control block", true
	}

	if startsLockGroup(curr) && !startsLockGroup(prev) {
		return "logical-block-spacing", "missing blank line before lock group", true
	}

	if isUnlockDefer(prev) && !isUnlockDefer(curr) {
		return "logical-block-spacing", "missing blank line after unlock defer", true
	}

	if isDefer(prev) && !isDefer(curr) {
		return "logical-block-spacing", "missing blank line after defer group", true
	}

	if isLoopOrSwitch(curr) && !isLoopOrSwitch(prev) {
		return "logical-block-spacing", "missing blank line before loop or switch", true
	}

	if isTestingRun(curr) && !isTestingRun(prev) {
		return "test-flow-spacing", "missing blank line before subtest block", true
	}

	if isTestAssertion(curr) && !isTestAssertion(prev) {
		return "test-flow-spacing", "missing blank line before test flow block", true
	}

	if isDecision(curr) && !isDecision(prev) {
		return "logical-block-spacing", "missing blank line before decision block", true
	}

	if isLast && isReturn(curr) {
		return "logical-block-spacing", "missing blank line before final return", true
	}

	return "", "", false
}

func isTestingRun(stmt ast.Stmt) bool {
	call, ok := stmtCall(stmt)

	if !ok {
		return false
	}

	_, name, ok := selectorCallName(call)

	return ok && name == "Run"
}

func isTestAssertion(stmt ast.Stmt) bool {
	call, ok := stmtCall(stmt)

	if !ok {
		return false
	}

	receiver, name, ok := selectorCallName(call)

	if !ok {
		return false
	}

	if receiver == "assert" || receiver == "require" {
		switch name {
		case "Equal", "NotEqual", "NoError", "Error", "ErrorIs", "ErrorAs",
			"True", "False", "Contains", "Len", "Empty", "NotEmpty",
			"Nil", "NotNil", "Fail", "FailNow":
			return true
		}
	}

	if receiver == "t" || receiver == "b" {
		switch name {
		case "Error", "Errorf", "Fatal", "Fatalf", "Fail", "FailNow":
			return true
		}
	}

	return false
}

func stmtCall(stmt ast.Stmt) (*ast.CallExpr, bool) {
	exprStmt, ok := stmt.(*ast.ExprStmt)

	if !ok {
		return nil, false
	}

	call, ok := exprStmt.X.(*ast.CallExpr)

	return call, ok
}

func selectorCallName(call *ast.CallExpr) (string, string, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)

	if !ok {
		return "", "", false
	}

	receiver, ok := selector.X.(*ast.Ident)

	if !ok {
		return "", "", false
	}

	return receiver.Name, selector.Sel.Name, true
}

func isStandaloneControl(stmt ast.Stmt) bool {
	switch current := stmt.(type) {
	case *ast.IfStmt:
		return current.Else == nil
	case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		return true
	default:
		return false
	}
}

func isDecision(stmt ast.Stmt) bool {
	current, ok := stmt.(*ast.IfStmt)

	if !ok {
		return false
	}

	return current.Else == nil
}

func startsLockGroup(stmt ast.Stmt) bool {
	expr, ok := stmt.(*ast.ExprStmt)

	if !ok {
		return false
	}

	call, ok := expr.X.(*ast.CallExpr)

	if !ok {
		return false
	}

	selector, ok := call.Fun.(*ast.SelectorExpr)

	if !ok {
		return false
	}

	return selector.Sel.Name == "Lock" || selector.Sel.Name == "RLock"
}

func isUnlockDefer(stmt ast.Stmt) bool {
	deferStmt, ok := stmt.(*ast.DeferStmt)

	if !ok {
		return false
	}

	selector, ok := deferStmt.Call.Fun.(*ast.SelectorExpr)

	if !ok {
		return false
	}

	return selector.Sel.Name == "Unlock" || selector.Sel.Name == "RUnlock"
}

func isDefer(stmt ast.Stmt) bool {
	_, ok := stmt.(*ast.DeferStmt)

	return ok
}

func isLoopOrSwitch(stmt ast.Stmt) bool {
	switch stmt.(type) {
	case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		return true
	default:
		return false
	}
}

func isReturn(stmt ast.Stmt) bool {
	_, ok := stmt.(*ast.ReturnStmt)

	return ok
}

func isCoupledErrorCheck(prev ast.Stmt, curr ast.Stmt) bool {
	name, ok := errorCheckName(curr)

	if !ok {
		return false
	}

	return stmtAssignsName(prev, name)
}

func errorCheckName(stmt ast.Stmt) (string, bool) {
	current, ok := stmt.(*ast.IfStmt)

	if !ok || current.Else != nil {
		return "", false
	}

	condition, ok := current.Cond.(*ast.BinaryExpr)

	if !ok || (condition.Op != token.EQL && condition.Op != token.NEQ) {
		return "", false
	}

	if name, ok := identNilPair(condition.X, condition.Y); ok {
		return name, true
	}

	return identNilPair(condition.Y, condition.X)
}

func identNilPair(candidate ast.Expr, nilCandidate ast.Expr) (string, bool) {
	ident, ok := candidate.(*ast.Ident)

	if !ok || ident.Name == "_" {
		return "", false
	}

	nilIdent, ok := nilCandidate.(*ast.Ident)

	if !ok || nilIdent.Name != "nil" {
		return "", false
	}

	return ident.Name, true
}

func stmtAssignsName(stmt ast.Stmt, name string) bool {
	switch current := stmt.(type) {
	case *ast.AssignStmt:
		for _, expr := range current.Lhs {
			if exprName(expr) == name {
				return true
			}
		}

	case *ast.DeclStmt:
		decl, ok := current.Decl.(*ast.GenDecl)

		if !ok {
			return false
		}

		for _, spec := range decl.Specs {
			values, ok := spec.(*ast.ValueSpec)

			if !ok {
				continue
			}

			for _, ident := range values.Names {
				if ident.Name == name {
					return true
				}
			}
		}
	}

	return false
}

func exprName(expr ast.Expr) string {
	switch current := expr.(type) {
	case *ast.Ident:
		return current.Name
	case *ast.ParenExpr:
		return exprName(current.X)
	default:
		return ""
	}
}

func insertionLine(prevEnd int, currStart int, comments []commentSpan) int {
	line := currStart

	for {
		start, ok := directlyAttachedCommentStart(prevEnd, line, comments)

		if !ok {
			return line
		}

		line = start
	}
}

func directlyAttachedCommentStart(prevEnd int, line int, comments []commentSpan) (int, bool) {
	for idx := len(comments) - 1; idx >= 0; idx-- {
		comment := comments[idx]

		if comment.end != line-1 || comment.start <= prevEnd {
			continue
		}

		return comment.start, true
	}

	return 0, false
}

func hasBlankBetweenLines(lines []string, prevEnd int, currStart int) bool {
	for line := prevEnd + 1; line < currStart; line++ {
		if line > 0 && line <= len(lines) && strings.TrimSpace(lines[line-1]) == "" {
			return true
		}
	}

	return false
}

func addInsertion(insertions map[int]insertion, line int, rule string, message string) {
	if _, exists := insertions[line]; exists {
		return
	}

	insertions[line] = insertion{rule: rule, message: message}
}

func applyInsertions(src []byte, insertions map[int]insertion) []byte {
	lines := splitLines(src)
	endsWithNewline := bytes.HasSuffix(src, []byte("\n"))
	out := make([]string, 0, len(lines)+len(insertions))

	for idx, line := range lines {
		lineNo := idx + 1

		if _, ok := insertions[lineNo]; ok && len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}

		out = append(out, line)
	}

	result := strings.Join(out, "\n")

	if endsWithNewline {
		result += "\n"
	}

	return []byte(result)
}

func min(a int, b int) int {
	if a < b {
		return a
	}

	return b
}
