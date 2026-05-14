package readability

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
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
	insertions := map[int]string{}

	ast.Inspect(file, func(node ast.Node) bool {
		switch current := node.(type) {
		case *ast.BlockStmt:
			checkStmtList(fset, lines, current.List, insertions)
		case *ast.CaseClause:
			checkStmtList(fset, lines, current.Body, insertions)
		case *ast.CommClause:
			checkStmtList(fset, lines, current.Body, insertions)
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

func CollectFiles(paths []string, includeHidden bool) ([]string, error) {
	if len(paths) == 0 {
		paths = []string{"."}
	}

	var files []string

	for _, path := range paths {
		path = normalizeRecursivePath(path)

		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", path, err)
		}

		if !info.IsDir() {
			if strings.HasSuffix(path, ".go") && !isGeneratedPath(path) {
				files = append(files, path)
			}

			continue
		}

		err = filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if current != path && entry.IsDir() && skipDir(entry.Name(), includeHidden) {
				return filepath.SkipDir
			}

			if !entry.IsDir() && strings.HasSuffix(current, ".go") && !isGeneratedPath(current) {
				files = append(files, current)
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
	header := strings.Join(lines[:min(20, len(lines))], "\n")

	return strings.Contains(header, "Code generated") && strings.Contains(header, "DO NOT EDIT")
}

func isGeneratedPath(path string) bool {
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	return IsGenerated(src)
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

func checkStmtList(fset *token.FileSet, lines []string, stmts []ast.Stmt, insertions map[int]string) {
	for idx := 1; idx < len(stmts); idx++ {
		prev := stmts[idx-1]
		curr := stmts[idx]

		if needsBlankBefore(prev, curr, idx == len(stmts)-1) && !hasBlankBetween(fset, lines, prev, curr) {
			line := fset.Position(curr.Pos()).Line
			insertions[line] = "missing blank line before logical block"
		}
	}
}

func needsBlankBefore(prev ast.Stmt, curr ast.Stmt, isLast bool) bool {
	if isStandaloneControl(prev) {
		return true
	}

	if startsLockGroup(curr) && !startsLockGroup(prev) {
		return true
	}

	if isUnlockDefer(prev) && !isUnlockDefer(curr) {
		return true
	}

	if isLoopOrSwitch(curr) && !isLoopOrSwitch(prev) {
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

func hasBlankBetween(fset *token.FileSet, lines []string, prev ast.Stmt, curr ast.Stmt) bool {
	prevEnd := fset.Position(prev.End()).Line
	currStart := fset.Position(curr.Pos()).Line

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
