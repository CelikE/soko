package output

import (
	"fmt"
	"strings"
)

// diffContext is the number of unchanged lines shown around each change.
const diffContext = 3

// DiffUnified returns a unified diff (with diffContext lines of context)
// describing the change from old to new, headed with the given label. It
// returns "" when old and new are byte-identical line-for-line. The diff is
// line-based via a longest-common-subsequence walk; no external dependency.
func DiffUnified(old, newContent []byte, label string) string {
	oldLines := splitLines(string(old))
	newLines := splitLines(string(newContent))
	ops := diffOps(oldLines, newLines)

	changed := false
	for _, op := range ops {
		if op.kind != ' ' {
			changed = true
			break
		}
	}
	if !changed {
		return ""
	}

	return formatUnified(ops, label)
}

// splitLines splits s into lines without their trailing newline. An empty
// string yields no lines; a trailing newline does not produce a spurious
// empty final line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// diffLine is one line of a diff: kind is ' ' (context), '-' (removed), or
// '+' (added).
type diffLine struct {
	kind byte
	text string
}

// diffOps returns the full line-level edit script from a to b via an LCS table.
func diffOps(a, b []string) []diffLine {
	m, n := len(a), len(b)
	// lcs[i][j] = length of the LCS of a[i:] and b[j:].
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var ops []diffLine
	i, j := 0, 0
	for i < m && j < n {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffLine{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffLine{'-', a[i]})
			i++
		default:
			ops = append(ops, diffLine{'+', b[j]})
			j++
		}
	}
	for ; i < m; i++ {
		ops = append(ops, diffLine{'-', a[i]})
	}
	for ; j < n; j++ {
		ops = append(ops, diffLine{'+', b[j]})
	}
	return ops
}

// formatUnified renders ops into a unified diff, grouping changes into hunks
// separated by more than 2*diffContext unchanged lines.
func formatUnified(ops []diffLine, label string) string {
	// Assign 1-based old/new line numbers to each op.
	type numberedOp struct {
		diffLine
		oldNo, newNo int
	}
	nums := make([]numberedOp, len(ops))
	oldNo, newNo := 0, 0
	for k, op := range ops {
		switch op.kind {
		case ' ':
			oldNo++
			newNo++
		case '-':
			oldNo++
		case '+':
			newNo++
		}
		nums[k] = numberedOp{op, oldNo, newNo}
	}

	var changed []int
	for k, op := range ops {
		if op.kind != ' ' {
			changed = append(changed, k)
		}
	}

	// Group change indices into hunks, padding diffContext lines on each side
	// and merging groups whose context windows touch.
	type span struct{ start, end int }
	var spans []span
	gs := max(changed[0]-diffContext, 0)
	ge := min(changed[0]+diffContext, len(ops)-1)
	for _, c := range changed[1:] {
		if c-diffContext <= ge+1 {
			ge = min(c+diffContext, len(ops)-1)
		} else {
			spans = append(spans, span{gs, ge})
			gs = max(c-diffContext, 0)
			ge = min(c+diffContext, len(ops)-1)
		}
	}
	spans = append(spans, span{gs, ge})

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n", label)
	fmt.Fprintf(&sb, "+++ %s\n", label)
	for _, s := range spans {
		oldStart, newStart, oldCount, newCount := 0, 0, 0, 0
		for k := s.start; k <= s.end; k++ {
			n := nums[k]
			if n.kind == ' ' || n.kind == '-' {
				if oldStart == 0 {
					oldStart = n.oldNo
				}
				oldCount++
			}
			if n.kind == ' ' || n.kind == '+' {
				if newStart == 0 {
					newStart = n.newNo
				}
				newCount++
			}
		}
		fmt.Fprintf(&sb, "@@ -%s +%s @@\n", hunkRange(oldStart, oldCount), hunkRange(newStart, newCount))
		for k := s.start; k <= s.end; k++ {
			fmt.Fprintf(&sb, "%c%s\n", ops[k].kind, ops[k].text)
		}
	}
	return sb.String()
}

// hunkRange formats a unified-diff range. A zero count uses start 0 ("0,0").
func hunkRange(start, count int) string {
	if count == 0 {
		return "0,0"
	}
	return fmt.Sprintf("%d,%d", start, count)
}
