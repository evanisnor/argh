package watches_test

import (
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/watches"
)

// ── ParseTrigger tests ────────────────────────────────────────────────────────

func TestParseTrigger_AllAtoms(t *testing.T) {
	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-25 * time.Hour)
	notStale := now.Add(-1 * time.Hour)

	tests := []struct {
		name       string
		expr       string
		snapshot   watches.PRSnapshot
		wantFire   bool
		wantErr    bool
		wantErrMsg string
	}{
		// ── ci-pass ──────────────────────────────────────────────────────────────
		{
			name:     "ci-pass fires when CIState=passing",
			expr:     "on:ci-pass",
			snapshot: watches.PRSnapshot{CIState: "passing"},
			wantFire: true,
		},
		{
			name:     "ci-pass does not fire when CIState=failing",
			expr:     "on:ci-pass",
			snapshot: watches.PRSnapshot{CIState: "failing"},
			wantFire: false,
		},
		// ── ci-fail ──────────────────────────────────────────────────────────────
		{
			name:     "ci-fail fires when CIState=failing",
			expr:     "on:ci-fail",
			snapshot: watches.PRSnapshot{CIState: "failing"},
			wantFire: true,
		},
		{
			name:     "ci-fail does not fire when CIState=passing",
			expr:     "on:ci-fail",
			snapshot: watches.PRSnapshot{CIState: "passing"},
			wantFire: false,
		},
		// ── approved (default N=1) ────────────────────────────────────────────────
		{
			name:     "approved fires when ApprovalCount>=1",
			expr:     "on:approved",
			snapshot: watches.PRSnapshot{ApprovalCount: 1},
			wantFire: true,
		},
		{
			name:     "approved fires when ApprovalCount>1",
			expr:     "on:approved",
			snapshot: watches.PRSnapshot{ApprovalCount: 3},
			wantFire: true,
		},
		{
			name:     "approved does not fire when ApprovalCount=0",
			expr:     "on:approved",
			snapshot: watches.PRSnapshot{ApprovalCount: 0},
			wantFire: false,
		},
		// ── approved:N ───────────────────────────────────────────────────────────
		{
			name:     "approved:2 fires when ApprovalCount>=2",
			expr:     "on:approved:2",
			snapshot: watches.PRSnapshot{ApprovalCount: 2},
			wantFire: true,
		},
		{
			name:     "approved:3 does not fire when ApprovalCount=2",
			expr:     "on:approved:3",
			snapshot: watches.PRSnapshot{ApprovalCount: 2},
			wantFire: false,
		},
		// ── all-threads-resolved ─────────────────────────────────────────────────
		{
			name:     "all-threads-resolved fires when AllThreadsResolved=true",
			expr:     "on:all-threads-resolved",
			snapshot: watches.PRSnapshot{AllThreadsResolved: true},
			wantFire: true,
		},
		{
			name:     "all-threads-resolved does not fire when AllThreadsResolved=false",
			expr:     "on:all-threads-resolved",
			snapshot: watches.PRSnapshot{AllThreadsResolved: false},
			wantFire: false,
		},
		// ── ready / open ─────────────────────────────────────────────────────────
		{
			name:     "on:ready fires when Status=open",
			expr:     "on:ready",
			snapshot: watches.PRSnapshot{Status: "open"},
			wantFire: true,
		},
		{
			name:     "on:ready does not fire when Status=draft",
			expr:     "on:ready",
			snapshot: watches.PRSnapshot{Status: "draft"},
			wantFire: false,
		},
		{
			name:     "on:open fires when Status=open (alias for on:ready)",
			expr:     "on:open",
			snapshot: watches.PRSnapshot{Status: "open"},
			wantFire: true,
		},
		{
			name:     "on:open does not fire when Status=draft",
			expr:     "on:open",
			snapshot: watches.PRSnapshot{Status: "draft"},
			wantFire: false,
		},
		{
			name:       "on:ready-for-review is now unknown",
			expr:       "on:ready-for-review",
			wantErr:    true,
			wantErrMsg: "unknown trigger atom",
		},
		// ── label-added ──────────────────────────────────────────────────────────
		{
			name:     "label-added:automerge fires when label present",
			expr:     "on:label-added:automerge",
			snapshot: watches.PRSnapshot{Labels: []string{"bug", "automerge"}},
			wantFire: true,
		},
		{
			name:     "label-added:automerge does not fire when label absent",
			expr:     "on:label-added:automerge",
			snapshot: watches.PRSnapshot{Labels: []string{"bug"}},
			wantFire: false,
		},
		{
			name:     "label-added:automerge does not fire with no labels",
			expr:     "on:label-added:automerge",
			snapshot: watches.PRSnapshot{},
			wantFire: false,
		},
		// ── label-removed ────────────────────────────────────────────────────────
		{
			name:     "label-removed:do-not-merge fires when label absent",
			expr:     "on:label-removed:do-not-merge",
			snapshot: watches.PRSnapshot{Labels: []string{"bug"}},
			wantFire: true,
		},
		{
			name:     "label-removed:do-not-merge does not fire when label present",
			expr:     "on:label-removed:do-not-merge",
			snapshot: watches.PRSnapshot{Labels: []string{"do-not-merge"}},
			wantFire: false,
		},
		// ── <Nh>-stale ───────────────────────────────────────────────────────────
		{
			name: "24h-stale fires when elapsed>=24h",
			expr: "on:24h-stale",
			snapshot: watches.PRSnapshot{
				LastActivityAt: stale,
				Now:            now,
			},
			wantFire: true,
		},
		{
			name: "24h-stale does not fire when elapsed<24h",
			expr: "on:24h-stale",
			snapshot: watches.PRSnapshot{
				LastActivityAt: notStale,
				Now:            now,
			},
			wantFire: false,
		},
		{
			name: "24h-stale does not fire when Now is zero",
			expr: "on:24h-stale",
			snapshot: watches.PRSnapshot{
				LastActivityAt: stale,
			},
			wantFire: false,
		},
		{
			name: "24h-stale does not fire when LastActivityAt is zero",
			expr: "on:24h-stale",
			snapshot: watches.PRSnapshot{
				Now: now,
			},
			wantFire: false,
		},
		// ── AND combinator ───────────────────────────────────────────────────────
		{
			name: "AND: approved+ci fires when both true",
			expr: "on:approved+ci-pass",
			snapshot: watches.PRSnapshot{
				ApprovalCount: 1,
				CIState:       "passing",
			},
			wantFire: true,
		},
		{
			name: "AND: approved+ci does not fire when only approved",
			expr: "on:approved+ci-pass",
			snapshot: watches.PRSnapshot{
				ApprovalCount: 1,
				CIState:       "failing",
			},
			wantFire: false,
		},
		{
			name: "AND: approved+ci does not fire when only ci passes",
			expr: "on:approved+ci-pass",
			snapshot: watches.PRSnapshot{
				ApprovalCount: 0,
				CIState:       "passing",
			},
			wantFire: false,
		},
		{
			name: "AND: approved+ci does not fire when neither true",
			expr: "on:approved+ci-pass",
			snapshot: watches.PRSnapshot{
				ApprovalCount: 0,
				CIState:       "failing",
			},
			wantFire: false,
		},
		// ── OR combinator ────────────────────────────────────────────────────────
		{
			name: "OR: ci-pass,approved fires when only ci passes",
			expr: "on:ci-pass,approved",
			snapshot: watches.PRSnapshot{
				CIState:       "passing",
				ApprovalCount: 0,
			},
			wantFire: true,
		},
		{
			name: "OR: ci-pass,approved fires when only approved",
			expr: "on:ci-pass,approved",
			snapshot: watches.PRSnapshot{
				CIState:       "failing",
				ApprovalCount: 1,
			},
			wantFire: true,
		},
		{
			name: "OR: ci-pass,approved fires when both true",
			expr: "on:ci-pass,approved",
			snapshot: watches.PRSnapshot{
				CIState:       "passing",
				ApprovalCount: 1,
			},
			wantFire: true,
		},
		{
			name: "OR: ci-pass,approved does not fire when both false",
			expr: "on:ci-pass,approved",
			snapshot: watches.PRSnapshot{
				CIState:       "failing",
				ApprovalCount: 0,
			},
			wantFire: false,
		},
		// ── Mixed AND+OR ─────────────────────────────────────────────────────────
		// "approved+ci-pass,24h-stale" → (approved AND ci-pass) OR (24h-stale)
		{
			name: "mixed AND+OR: fires on AND branch",
			expr: "on:approved+ci-pass,24h-stale",
			snapshot: watches.PRSnapshot{
				ApprovalCount: 1,
				CIState:       "passing",
				LastActivityAt: notStale,
				Now:            now,
			},
			wantFire: true,
		},
		{
			name: "mixed AND+OR: fires on OR branch (stale)",
			expr: "on:approved+ci-pass,24h-stale",
			snapshot: watches.PRSnapshot{
				ApprovalCount: 0,
				CIState:       "failing",
				LastActivityAt: stale,
				Now:            now,
			},
			wantFire: true,
		},
		{
			name: "mixed AND+OR: does not fire when AND partial and stale false",
			expr: "on:approved+ci-pass,24h-stale",
			snapshot: watches.PRSnapshot{
				ApprovalCount: 1,
				CIState:       "failing",
				LastActivityAt: notStale,
				Now:            now,
			},
			wantFire: false,
		},
		// ── Invalid expressions ───────────────────────────────────────────────────
		{
			name:       "missing on: prefix",
			expr:       "ci-pass",
			wantErr:    true,
			wantErrMsg: "trigger expression must start with",
		},
		{
			name:       "empty body after on:",
			expr:       "on:",
			wantErr:    true,
			wantErrMsg: "trigger expression has no conditions",
		},
		{
			name:       "unknown atom",
			expr:       "on:invalid-atom",
			wantErr:    true,
			wantErrMsg: "unknown trigger atom",
		},
		{
			name:       "approved:0 is invalid",
			expr:       "on:approved:0",
			wantErr:    true,
			wantErrMsg: "invalid approval count",
		},
		{
			name:       "approved:abc is invalid",
			expr:       "on:approved:abc",
			wantErr:    true,
			wantErrMsg: "invalid approval count",
		},
		{
			name:       "label-added without label name",
			expr:       "on:label-added:",
			wantErr:    true,
			wantErrMsg: "label name is required",
		},
		{
			name:       "label-removed without label name",
			expr:       "on:label-removed:",
			wantErr:    true,
			wantErrMsg: "label name is required",
		},
		{
			name:       "trailing comma creates empty OR condition",
			expr:       "on:ci-pass,",
			wantErr:    true,
			wantErrMsg: "empty condition",
		},
		{
			name:       "trailing plus creates empty AND atom",
			expr:       "on:ci-pass+",
			wantErr:    true,
			wantErrMsg: "empty atom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := watches.ParseTrigger(tt.expr)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseTrigger(%q) expected error containing %q, got nil", tt.expr, tt.wantErrMsg)
				}
				if tt.wantErrMsg != "" && !containsString(err.Error(), tt.wantErrMsg) {
					t.Fatalf("ParseTrigger(%q) error = %q, want to contain %q", tt.expr, err.Error(), tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTrigger(%q) unexpected error: %v", tt.expr, err)
			}
			got := node.Evaluate(tt.snapshot)
			if got != tt.wantFire {
				t.Errorf("Evaluate(%q) = %v, want %v", tt.expr, got, tt.wantFire)
			}
		})
	}
}

// TestParseTrigger_ASTStructure verifies the parsed AST shape for representative expressions.
func TestParseTrigger_ASTStructure(t *testing.T) {
	t.Run("single atom returns AtomNode", func(t *testing.T) {
		node, err := watches.ParseTrigger("on:ci-pass")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		atom, ok := node.(*watches.AtomNode)
		if !ok {
			t.Fatalf("expected *AtomNode, got %T", node)
		}
		if atom.Atom.Kind != watches.AtomCIPass {
			t.Errorf("expected AtomCIPass, got %v", atom.Atom.Kind)
		}
	})

	t.Run("AND expression returns ANDNode", func(t *testing.T) {
		node, err := watches.ParseTrigger("on:approved+ci-pass")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		and, ok := node.(*watches.ANDNode)
		if !ok {
			t.Fatalf("expected *ANDNode, got %T", node)
		}
		if len(and.Children) != 2 {
			t.Errorf("expected 2 AND children, got %d", len(and.Children))
		}
	})

	t.Run("OR expression returns ORNode", func(t *testing.T) {
		node, err := watches.ParseTrigger("on:ci-pass,approved")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		or, ok := node.(*watches.ORNode)
		if !ok {
			t.Fatalf("expected *ORNode, got %T", node)
		}
		if len(or.Children) != 2 {
			t.Errorf("expected 2 OR children, got %d", len(or.Children))
		}
	})

	t.Run("mixed AND+OR: OR wraps ANDNode and AtomNode", func(t *testing.T) {
		node, err := watches.ParseTrigger("on:approved+ci-pass,24h-stale")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		or, ok := node.(*watches.ORNode)
		if !ok {
			t.Fatalf("expected *ORNode, got %T", node)
		}
		if len(or.Children) != 2 {
			t.Fatalf("expected 2 OR children, got %d", len(or.Children))
		}
		if _, ok := or.Children[0].(*watches.ANDNode); !ok {
			t.Errorf("expected first OR child to be *ANDNode, got %T", or.Children[0])
		}
		if _, ok := or.Children[1].(*watches.AtomNode); !ok {
			t.Errorf("expected second OR child to be *AtomNode, got %T", or.Children[1])
		}
	})

	t.Run("approved:N parses N correctly", func(t *testing.T) {
		node, err := watches.ParseTrigger("on:approved:3")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		atom, ok := node.(*watches.AtomNode)
		if !ok {
			t.Fatalf("expected *AtomNode, got %T", node)
		}
		if atom.Atom.ApprovalN != 3 {
			t.Errorf("expected ApprovalN=3, got %d", atom.Atom.ApprovalN)
		}
	})

	t.Run("Nh-stale parses duration correctly", func(t *testing.T) {
		node, err := watches.ParseTrigger("on:48h-stale")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		atom, ok := node.(*watches.AtomNode)
		if !ok {
			t.Fatalf("expected *AtomNode, got %T", node)
		}
		if atom.Atom.Kind != watches.AtomStale {
			t.Errorf("expected AtomStale, got %v", atom.Atom.Kind)
		}
		if atom.Atom.StaleDuration != 48*time.Hour {
			t.Errorf("expected 48h duration, got %v", atom.Atom.StaleDuration)
		}
	})

	t.Run("label-added:<name> captures label name", func(t *testing.T) {
		node, err := watches.ParseTrigger("on:label-added:automerge")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		atom, ok := node.(*watches.AtomNode)
		if !ok {
			t.Fatalf("expected *AtomNode, got %T", node)
		}
		if atom.Atom.Kind != watches.AtomLabelAdded {
			t.Errorf("expected AtomLabelAdded, got %v", atom.Atom.Kind)
		}
		if atom.Atom.LabelName != "automerge" {
			t.Errorf("expected LabelName=automerge, got %q", atom.Atom.LabelName)
		}
	})
}

// TestAtomNode_Evaluate_TimeBased explicitly tests boundary conditions for stale.
func TestAtomNode_Evaluate_TimeBased(t *testing.T) {
	base := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		expr           string
		lastActivityAt time.Time
		now            time.Time
		want           bool
	}{
		{
			name:           "elapsed exactly equals duration → fires",
			expr:           "on:24h-stale",
			lastActivityAt: base.Add(-24 * time.Hour),
			now:            base,
			want:           true,
		},
		{
			name:           "elapsed just under duration → does not fire",
			expr:           "on:24h-stale",
			lastActivityAt: base.Add(-24*time.Hour + time.Second),
			now:            base,
			want:           false,
		},
		{
			name:           "elapsed exceeds duration → fires",
			expr:           "on:24h-stale",
			lastActivityAt: base.Add(-72 * time.Hour),
			now:            base,
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := watches.ParseTrigger(tt.expr)
			if err != nil {
				t.Fatalf("ParseTrigger(%q) unexpected error: %v", tt.expr, err)
			}
			snap := watches.PRSnapshot{
				LastActivityAt: tt.lastActivityAt,
				Now:            tt.now,
			}
			got := node.Evaluate(snap)
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestAtomNode_Evaluate_UnknownKind covers the fallthrough return false in AtomNode.Evaluate.
func TestAtomNode_Evaluate_UnknownKind(t *testing.T) {
	node := &watches.AtomNode{Atom: watches.Atom{Kind: watches.AtomKind(999)}}
	if node.Evaluate(watches.PRSnapshot{}) {
		t.Error("expected false for unknown AtomKind, got true")
	}
}

// TestAtomNode_Evaluate_ApprovedDefaultN covers the n<=0 defensive branch in approved evaluation.
func TestAtomNode_Evaluate_ApprovedDefaultN(t *testing.T) {
	// ApprovalN=0 should default to requiring 1 approval.
	node := &watches.AtomNode{Atom: watches.Atom{Kind: watches.AtomApproved, ApprovalN: 0}}
	if node.Evaluate(watches.PRSnapshot{ApprovalCount: 0}) {
		t.Error("expected false when ApprovalN=0 (defaults to 1) and ApprovalCount=0")
	}
	if !node.Evaluate(watches.PRSnapshot{ApprovalCount: 1}) {
		t.Error("expected true when ApprovalN=0 (defaults to 1) and ApprovalCount=1")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
