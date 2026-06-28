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
	Message string
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
	insertions := map[int]string{}

	ast.Inspect(file, func(node ast.Node) bool {
		switch current := node.(type) {
		case *ast.BlockStmt:
			checkStmtList(fset, lines, comments, current.List, insertions)
		case *ast.CaseClause:
			checkStmtList(fset, lines, comments, current.Body, insertions)
		case *ast.CommClause:
			checkStmtList(fset, lines, comments, current.Body, insertions)
		}

		return true
	})

	issues := make([]Issue, 0, len(insertions))

	for line, message := range insertions {
		issues = append(issues, Issue{File: path, Line: line, Message: message})
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

func checkStmtList(
	fset *token.FileSet,
	lines []string,
	comments []commentSpan,
	stmts []ast.Stmt,
	insertions map[int]string,
) {
	for idx := 1; idx < len(stmts); idx++ {
		prev := stmts[idx-1]
		curr := stmts[idx]

		if needsBlankBefore(prev, curr, idx == len(stmts)-1) {
			prevEnd := fset.Position(prev.End()).Line
			currStart := fset.Position(curr.Pos()).Line
			line := insertionLine(prevEnd, currStart, comments)

			if hasBlankBetweenLines(lines, prevEnd, line) {
				continue
			}

			insertions[line] = "missing blank line before logical block"
		}
	}
}

func needsBlankBefore(prev ast.Stmt, curr ast.Stmt, isLast bool) bool {
	if isCoupledErrorCheck(prev, curr) {
		return false
	}

	if isStandaloneControl(prev) {
		return true
	}

	if startsLockGroup(curr) && !startsLockGroup(prev) {
		return true
	}

	if isUnlockDefer(prev) && !isUnlockDefer(curr) {
		return true
	}

	if isDefer(prev) && !isDefer(curr) {
		return true
	}

	if isLoopOrSwitch(curr) && !isLoopOrSwitch(prev) {
		return true
	}

	if isDecision(curr) && !isDecision(prev) {
		return true
	}

	if isLast && isReturn(curr) {
		return true
	}

	return false
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

func applyInsertions(src []byte, insertions map[int]string) []byte {
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
