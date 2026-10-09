package sourcemap

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// ASTContext is an opaque, registry-local proof of one complete, nonempty
// expansion context. The zero value is unknown. Equal-looking stacks do not
// make two handles interchangeable.
type ASTContext struct {
	registry *ASTEvidence
	proof    *astContextProof
}

type astContextProof struct {
	record *astRecord
	token  *astTokenEvidence
}

type astTokenEvidence struct {
	kind      token.Token
	text      string
	origin    *Span
	stack     []Expansion
	mapping   string
	proof     *astContextProof
	rewritten bool
}

type astRecord struct {
	input    bool
	unknown  bool
	conflict bool
	cause    ASTContext
	tokens   map[TokenSlot]*astTokenEvidence
}

// ASTEvidence freezes one input stage and records explicit causes for newly
// constructed syntax. It is local to one transformation, not a global cache.
type ASTEvidence struct {
	stages  []string
	records map[ast.Node]*astRecord
}

// CaptureAST requires the matching parser FileSet, root and map from one input
// stage. Go AST nodes carry no FileSet identity themselves; callers must retain
// that pairing. Bounds and lexical token checks reject invalid input slots.
// Every input node gets a record, including nodes without known provenance.
func (m *Map) CaptureAST(fs *token.FileSet, root ast.Node) *ASTEvidence {
	e := &ASTEvidence{records: map[ast.Node]*astRecord{}}
	if m != nil {
		e.stages = append([]string(nil), m.Stages...)
	}
	if nilASTNode(root) {
		return e
	}
	var owner *token.File
	if m != nil && fs != nil {
		owner = fs.File(root.Pos())
		if owner != nil && owner.Size() != len(m.Source) {
			owner = nil
		}
		if file, ok := root.(*ast.File); ok && owner != nil {
			if file.FileStart != owner.Pos(0) || file.FileEnd != owner.Pos(owner.Size()) {
				owner = nil
			}
		}
	}
	var lexed map[int]scannedASTToken
	if owner != nil {
		lexed = scanASTTokens(m.Source)
	}
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if _, present := e.records[n]; present {
			return false
		}
		r := &astRecord{input: true, tokens: map[TokenSlot]*astTokenEvidence{}}
		e.records[n] = r
		for _, slot := range astTokens(n) {
			frozen := &astTokenEvidence{kind: slot.kind, text: slot.text, mapping: "generated/unmapped"}
			frozen.proof = &astContextProof{record: r, token: frozen}
			r.tokens[slot.slot] = frozen
			if owner == nil || !slot.pos.IsValid() || fs.File(slot.pos) != owner {
				continue
			}
			t, ok := lexed[owner.Offset(slot.pos)]
			if !ok || t.kind != slot.kind || t.text != slot.text {
				continue
			}
			segment := m.astTokenSegment(t)
			if segment == nil {
				continue
			}
			frozen.stack = append([]Expansion(nil), segment.Stack...)
			if segment.Start == t.start && segment.End == t.end && validASTSpan(segment.Origin) {
				frozen.origin = copyASTSpan(segment.Origin)
				frozen.mapping = segment.Kind
			}
		}
		return true
	})
	return e
}

// AddInput admits a freshly parsed fragment with its own matching source map.
// Nodes are joined by identity, never by positions or equal text. Existing
// records cannot be replaced, including unknown or conflicting records.
func (e *ASTEvidence) AddInput(fs *token.FileSet, root ast.Node, mapping *Map) bool {
	if e == nil || nilASTNode(root) || mapping == nil {
		return false
	}
	input := mapping.CaptureAST(fs, root)
	for node := range input.records {
		if e.records[node] != nil {
			return false
		}
	}
	if e.records == nil {
		e.records = map[ast.Node]*astRecord{}
	}
	for node, record := range input.records {
		e.records[node] = record
	}
	return true
}

// One winning segment must cover the entire token. A later partial overlay
// invalidates both proofs; contexts from separately covering pieces cannot be
// merged by comparing their stack values. A wide winner can still prove context.
func (m *Map) astTokenSegment(t scannedASTToken) *Segment {
	for i := len(m.Segments) - 1; i >= 0; i-- {
		s := &m.Segments[i]
		// Empty or reversed intervals cover no byte, just as in Map.Lookup.
		if s.Start >= s.End {
			continue
		}
		if s.Start < t.end && s.End > t.start {
			if s.Start <= t.start && s.End >= t.end {
				return s
			}
			return nil
		}
	}
	return nil
}

func validASTSpan(span *Span) bool {
	return span != nil && span.File != "" && span.Start >= 0 && span.End > span.Start && span.Line >= 1 && span.Column >= 1
}

// Context returns only the explicitly selected token's frozen context. It
// never substitutes node.Pos, a child, a parent or a neighboring source token.
func (e *ASTEvidence) Context(node ast.Node, slot TokenSlot) ASTContext {
	if e == nil {
		return ASTContext{}
	}
	r := e.records[node]
	if r == nil || r.tokens[slot] == nil {
		return ASTContext{}
	}
	ctx := ASTContext{e, r.tokens[slot].proof}
	if len(e.contextStack(ctx)) == 0 {
		return ASTContext{}
	}
	return ctx
}

func (e *ASTEvidence) contextStack(ctx ASTContext) []Expansion {
	seen := map[*astRecord]bool{}
	for ctx.registry == e && ctx.proof != nil {
		r := ctx.proof.record
		if r == nil || seen[r] || r.unknown || r.conflict {
			return nil
		}
		seen[r] = true
		if r.input {
			return ctx.proof.token.stack
		}
		ctx = r.cause
	}
	return nil
}

func (e *ASTEvidence) newGenerated(node ast.Node, ctx ASTContext, unknown bool) {
	if e.records == nil {
		e.records = map[ast.Node]*astRecord{}
	}
	r := &astRecord{cause: ctx, unknown: unknown, tokens: map[TokenSlot]*astTokenEvidence{}}
	for _, slot := range astTokens(node) {
		t := &astTokenEvidence{kind: slot.kind, text: slot.text, mapping: "generated/unmapped"}
		t.proof = &astContextProof{record: r, token: t}
		r.tokens[slot.slot] = t
	}
	e.records[node] = r
}

// Generated registers this new node's own token slots, never its descendants.
// Input nodes are immutable records. Unknown registrations and conflicting
// causes leave permanent tombstones; the same valid handle is idempotent.
func (e *ASTEvidence) Generated(node ast.Node, ctx ASTContext) bool {
	if e == nil || nilASTNode(node) {
		return false
	}
	known := len(e.contextStack(ctx)) > 0
	if r := e.records[node]; r != nil {
		if r.input || r.unknown || r.conflict {
			return false
		}
		if known && r.cause == ctx {
			return true
		}
		r.conflict = true
		return false
	}
	e.newGenerated(node, ctx, !known)
	return known
}

// Observe mutations even when parsing or matching the printed stage fails.
// Losing or changing a slot is irreversible point invalidation for this
// snapshot, independently of whether its frozen context remains usable.
func (e *ASTEvidence) observeRewrites(root ast.Node) {
	if nilASTNode(root) {
		return
	}
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		r := e.records[n]
		if r == nil || !r.input {
			return true
		}
		current := map[TokenSlot]astToken{}
		for _, slot := range astTokens(n) {
			current[slot.slot] = slot
		}
		for slot, frozen := range r.tokens {
			now, present := current[slot]
			if !present || frozen.kind != now.kind || frozen.text != now.text {
				frozen.rewritten = true
			}
		}
		return true
	})
}

// GeneratedTree registers a closed, newly constructed compiler template before
// source operands are spliced into it. Any existing record rejects the whole
// tree: existing records stay intact and all new nodes become unknown. The
// caller, not positions or spelling, warrants that these are generated nodes.
func (e *ASTEvidence) GeneratedTree(root ast.Node, ctx ASTContext) bool {
	if e == nil || nilASTNode(root) {
		return false
	}
	var nodes []ast.Node
	seen := map[ast.Node]bool{}
	closed := true
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil || seen[n] {
			return false
		}
		seen[n] = true
		nodes = append(nodes, n)
		if e.records[n] != nil {
			closed = false
		}
		return true
	})
	known := closed && len(e.contextStack(ctx)) > 0
	for _, n := range nodes {
		if e.records[n] == nil {
			e.newGenerated(n, ctx, !known)
		}
	}
	return known
}

// Print transports frozen evidence through a complete AST/printed-AST
// correspondence. New syntax has only explicitly registered context. A changed
// original token loses its point origin, while its frozen context can survive.
// Parenthesis chains with changed lengths contribute no parenthesis evidence.
func (e *ASTEvidence) Print(root ast.Node, after []byte, stage string) *Map {
	next := &Map{Source: append([]byte(nil), after...)}
	if e == nil {
		return next
	}
	next.Stages = append(append([]string(nil), e.stages...), stage)
	e.observeRewrites(root)
	fs := token.NewFileSet()
	parsed, err := parser.ParseFile(fs, "", after, 0)
	if err != nil || nilASTNode(root) {
		return next
	}
	var match astCorrespondence
	if !match.node(root, parsed) {
		return next
	}
	lexed := scanASTTokens(after)
	type claim struct {
		index    int
		proof    *astContextProof
		conflict bool
	}
	claims := map[int]claim{}
	for _, pair := range match.pairs {
		r := e.records[pair.before]
		if r == nil {
			continue
		}
		current := map[TokenSlot]astToken{}
		for _, slot := range astTokens(pair.before) {
			current[slot.slot] = slot
		}
		for _, slot := range astTokens(pair.after) {
			frozen := r.tokens[slot.slot]
			before, exists := current[slot.slot]
			if frozen == nil || !exists || before.kind != slot.kind || before.text != slot.text || !slot.pos.IsValid() {
				continue
			}
			offset := fs.PositionFor(slot.pos, false).Offset
			t, ok := lexed[offset]
			if !ok || t.kind != slot.kind || t.text != slot.text {
				continue
			}
			if old, exists := claims[offset]; exists {
				if old.proof != frozen.proof || old.conflict {
					old.conflict = true
					claims[offset] = old
					next.Segments[old.index] = Segment{Start: t.start, End: t.end, Kind: "generated/unmapped"}
				}
				continue
			}
			s := Segment{Start: t.start, End: t.end, Kind: "generated/unmapped"}
			s.Stack = append([]Expansion(nil), e.contextStack(ASTContext{e, frozen.proof})...)
			if r.input && !frozen.rewritten && frozen.kind == slot.kind && frozen.text == slot.text {
				s.Origin = copyASTSpan(frozen.origin)
				if s.Origin != nil {
					s.Kind = frozen.mapping
				}
			}
			claims[offset] = claim{index: len(next.Segments), proof: frozen.proof}
			next.Segments = append(next.Segments, s)
		}
	}
	return next
}

func copyASTSpan(span *Span) *Span {
	if span == nil {
		return nil
	}
	copy := *span
	return &copy
}
