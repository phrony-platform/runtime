package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/cliout"
	"github.com/spf13/cobra"
)

func newAgentDiffCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "diff MANIFEST AGENT@VERSION",
		Short: "Diff a local manifest against a published version",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd, runtimeAddr, args[0], args[1])
		},
	}
}

func runDiff(cmd *cobra.Command, runtimeAddr *string, manifestPath, remoteRef string) error {
	resolved, err := loadResolvedManifest(manifestPath)
	if err != nil {
		return err
	}
	localJSON, err := resolved.JSON()
	if err != nil {
		return fmt.Errorf("encode resolved manifest: %w", err)
	}

	ref, err := parseAgentRefVersionRequired(remoteRef)
	if err != nil {
		return err
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.GetAgentVersion(cmd.Context(), &runtimev1.GetAgentVersionRequest{
			AgentRef: ref,
		})
		if err != nil {
			return clierr.WrapRPC("get agent version", err)
		}

		localPretty, err := prettyCanonicalJSON(localJSON)
		if err != nil {
			return fmt.Errorf("canonicalize local manifest: %w", err)
		}
		remotePretty, err := prettyCanonicalJSON(resp.GetManifest())
		if err != nil {
			return fmt.Errorf("canonicalize remote manifest: %w", err)
		}

		agentLabel := agentref.FormatVersioned(ref.GetNamespace(), ref.GetName(), ref.GetVersion())
		out := cmd.OutOrStdout()
		if bytes.Equal(localPretty, remotePretty) {
			c := newDiffColors(cliout.UseColor(out))
			fmt.Fprintf(out, "%s local manifest matches %s\n", c.equal("No differences."), c.bold(agentLabel))
			return nil
		}

		writeManifestDiff(out, diffOptions{
			localLabel:  manifestPath,
			remoteLabel: agentLabel,
			// The published version is the "from" side; the local manifest is the
			// "to" side, so additions/deletions read as "what publishing would change".
			from:  string(remotePretty),
			to:    string(localPretty),
			color: cliout.UseColor(out),
		})
		return nil
	})
}

// prettyCanonicalJSON normalizes raw JSON into indented form with sorted object
// keys so structurally identical manifests produce identical, line-comparable text.
func prettyCanonicalJSON(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

type diffOptions struct {
	localLabel  string
	remoteLabel string
	from        string
	to          string
	color       bool
}

// diffContext is the number of unchanged lines shown around each change.
const diffContext = 3

func writeManifestDiff(w io.Writer, opts diffOptions) {
	fromLines := strings.Split(opts.from, "\n")
	toLines := strings.Split(opts.to, "\n")
	ops := diffLines(fromLines, toLines)

	adds, dels := 0, 0
	for _, op := range ops {
		switch op.kind {
		case opInsert:
			adds++
		case opDelete:
			dels++
		}
	}

	c := newDiffColors(opts.color)
	fmt.Fprintf(w, "%s\n", c.del("--- "+opts.remoteLabel+" (published)"))
	fmt.Fprintf(w, "%s\n", c.add("+++ "+opts.localLabel+" (local)"))

	writeHunks(w, ops, c)

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s: %s, %s\n",
		c.bold("Summary"),
		c.add(plural(adds, "addition", "additions")),
		c.del(plural(dels, "deletion", "deletions")),
	)
}

// diffRecord is a single diff line annotated with its 1-based line numbers on
// each side, used to render unified hunk headers.
type diffRecord struct {
	op           diffLine
	fromNo, toNo int
}

func writeHunks(w io.Writer, ops []diffLine, c diffColors) {
	if len(ops) == 0 {
		return
	}

	recs := make([]diffRecord, len(ops))
	fromNo, toNo := 1, 1
	for i, op := range ops {
		recs[i] = diffRecord{op: op, fromNo: fromNo, toNo: toNo}
		switch op.kind {
		case opEqual:
			fromNo++
			toNo++
		case opDelete:
			fromNo++
		case opInsert:
			toNo++
		}
	}

	// Keep every change plus up to diffContext unchanged lines around it.
	keep := make([]bool, len(ops))
	for i, op := range ops {
		if op.kind == opEqual {
			continue
		}
		lo := i - diffContext
		if lo < 0 {
			lo = 0
		}
		hi := i + diffContext
		if hi >= len(ops) {
			hi = len(ops) - 1
		}
		for k := lo; k <= hi; k++ {
			keep[k] = true
		}
	}

	for s := 0; s < len(ops); {
		if !keep[s] {
			s++
			continue
		}
		e := s
		for e < len(ops) && keep[e] {
			e++
		}
		writeHunk(w, recs[s:e], c)
		s = e
	}
}

func writeHunk(w io.Writer, recs []diffRecord, c diffColors) {
	fromCount, toCount := 0, 0
	for _, r := range recs {
		switch r.op.kind {
		case opEqual:
			fromCount++
			toCount++
		case opDelete:
			fromCount++
		case opInsert:
			toCount++
		}
	}

	fromStart, toStart := recs[0].fromNo, recs[0].toNo
	fmt.Fprintf(w, "%s\n", c.hunk(fmt.Sprintf("@@ -%d,%d +%d,%d @@", fromStart, fromCount, toStart, toCount)))

	for _, r := range recs {
		switch r.op.kind {
		case opEqual:
			fmt.Fprintf(w, "  %s\n", r.op.text)
		case opDelete:
			fmt.Fprintf(w, "%s\n", c.del("- "+r.op.text))
		case opInsert:
			fmt.Fprintf(w, "%s\n", c.add("+ "+r.op.text))
		}
	}
}

type diffOpKind int

const (
	opEqual diffOpKind = iota
	opDelete
	opInsert
)

type diffLine struct {
	kind diffOpKind
	text string
}

// diffLines computes a line-level diff between from and to using a longest
// common subsequence, so inserted or removed lines don't cascade into spurious
// changes on every following line.
func diffLines(from, to []string) []diffLine {
	n, m := len(from), len(to)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if from[i] == to[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var out []diffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case from[i] == to[j]:
			out = append(out, diffLine{opEqual, from[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, diffLine{opDelete, from[i]})
			i++
		default:
			out = append(out, diffLine{opInsert, to[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, diffLine{opDelete, from[i]})
	}
	for ; j < m; j++ {
		out = append(out, diffLine{opInsert, to[j]})
	}
	return out
}

type diffColors struct {
	enabled bool
}

func newDiffColors(enabled bool) diffColors {
	return diffColors{enabled: enabled}
}

func (c diffColors) wrap(code, s string) string {
	if !c.enabled {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (c diffColors) add(s string) string   { return c.wrap("32", s) }
func (c diffColors) del(s string) string   { return c.wrap("31", s) }
func (c diffColors) hunk(s string) string  { return c.wrap("36", s) }
func (c diffColors) equal(s string) string { return c.wrap("32", s) }
func (c diffColors) bold(s string) string  { return c.wrap("1", s) }

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
