// Package watches provides watch trigger and action parsing for argh.
package watches

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PRSnapshot is the PR state evaluated against a trigger expression.
// Now is the current wall-clock time; inject for testability.
type PRSnapshot struct {
	Status             string
	CIState            string
	ApprovalCount      int
	AllThreadsResolved bool
	Labels             []string
	LastActivityAt     time.Time
	Now                time.Time
}

// TriggerNode is a node in the trigger expression AST.
type TriggerNode interface {
	// Evaluate returns true when the trigger condition is satisfied.
	Evaluate(snapshot PRSnapshot) bool
}

// AtomKind identifies the kind of a trigger atom.
type AtomKind int

const (
	AtomCIPass AtomKind = iota
	AtomCIFail
	AtomApproved
	AtomAllThreadsResolved
	AtomReadyForReview
	AtomLabelAdded
	AtomLabelRemoved
	AtomStale
)

// Atom is a single trigger condition.
type Atom struct {
	Kind          AtomKind
	ApprovalN     int           // for AtomApproved; 0 means default (1)
	LabelName     string        // for AtomLabelAdded and AtomLabelRemoved
	StaleDuration time.Duration // for AtomStale
}

// AtomNode wraps a single Atom as a TriggerNode.
type AtomNode struct {
	Atom Atom
}

// Evaluate returns true when the atom's condition is satisfied by snapshot.
func (a *AtomNode) Evaluate(snapshot PRSnapshot) bool {
	switch a.Atom.Kind {
	case AtomCIPass:
		return snapshot.CIState == "passing"
	case AtomCIFail:
		return snapshot.CIState == "failing"
	case AtomApproved:
		n := a.Atom.ApprovalN
		if n <= 0 {
			n = 1
		}
		return snapshot.ApprovalCount >= n
	case AtomAllThreadsResolved:
		return snapshot.AllThreadsResolved
	case AtomReadyForReview:
		return snapshot.Status == "open"
	case AtomLabelAdded:
		for _, l := range snapshot.Labels {
			if l == a.Atom.LabelName {
				return true
			}
		}
		return false
	case AtomLabelRemoved:
		for _, l := range snapshot.Labels {
			if l == a.Atom.LabelName {
				return false
			}
		}
		return true
	case AtomStale:
		if snapshot.Now.IsZero() || snapshot.LastActivityAt.IsZero() {
			return false
		}
		return snapshot.Now.Sub(snapshot.LastActivityAt) >= a.Atom.StaleDuration
	}
	return false
}

// ANDNode fires when all children evaluate to true simultaneously.
type ANDNode struct {
	Children []TriggerNode
}

// Evaluate returns true only when every child evaluates to true.
func (n *ANDNode) Evaluate(snapshot PRSnapshot) bool {
	for _, child := range n.Children {
		if !child.Evaluate(snapshot) {
			return false
		}
	}
	return true
}

// ORNode fires when any child evaluates to true.
type ORNode struct {
	Children []TriggerNode
}

// Evaluate returns true when at least one child evaluates to true.
func (n *ORNode) Evaluate(snapshot PRSnapshot) bool {
	for _, child := range n.Children {
		if child.Evaluate(snapshot) {
			return true
		}
	}
	return false
}

var staleRegexp = regexp.MustCompile(`^(\d+)h-stale$`)

// ParseTrigger parses a trigger expression of the form "on:<expr>" and returns
// the root AST node. The "," operator (OR) has lower precedence than "+"
// (AND), so "a+b,c" is parsed as (a AND b) OR c.
func ParseTrigger(expr string) (TriggerNode, error) {
	expr = strings.TrimSpace(expr)
	if !strings.HasPrefix(expr, "on:") {
		return nil, fmt.Errorf("trigger expression must start with \"on:\": %q", expr)
	}
	body := strings.TrimPrefix(expr, "on:")
	if body == "" {
		return nil, fmt.Errorf("trigger expression has no conditions after \"on:\": %q", expr)
	}
	return parseBody(body, expr)
}

// parseBody parses the body of a trigger expression after "on:".
func parseBody(body, originalExpr string) (TriggerNode, error) {
	// Split on "," for OR (lower precedence).
	orParts := strings.Split(body, ",")
	orChildren := make([]TriggerNode, 0, len(orParts))

	for _, part := range orParts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty condition in trigger expression: %q", originalExpr)
		}

		// Split on "+" for AND (higher precedence).
		andParts := strings.Split(part, "+")
		andChildren := make([]TriggerNode, 0, len(andParts))

		for _, atomStr := range andParts {
			atomStr = strings.TrimSpace(atomStr)
			if atomStr == "" {
				return nil, fmt.Errorf("empty atom in trigger expression: %q", originalExpr)
			}
			node, err := parseAtom(atomStr)
			if err != nil {
				return nil, err
			}
			andChildren = append(andChildren, node)
		}

		if len(andChildren) == 1 {
			orChildren = append(orChildren, andChildren[0])
		} else {
			orChildren = append(orChildren, &ANDNode{Children: andChildren})
		}
	}

	if len(orChildren) == 1 {
		return orChildren[0], nil
	}
	return &ORNode{Children: orChildren}, nil
}

// parseAtom parses a single trigger atom string into an AtomNode.
func parseAtom(atom string) (*AtomNode, error) {
	switch {
	case atom == "ci-pass":
		return &AtomNode{Atom: Atom{Kind: AtomCIPass}}, nil

	case atom == "ci-fail":
		return &AtomNode{Atom: Atom{Kind: AtomCIFail}}, nil

	case atom == "approved":
		return &AtomNode{Atom: Atom{Kind: AtomApproved, ApprovalN: 1}}, nil

	case strings.HasPrefix(atom, "approved:"):
		s := strings.TrimPrefix(atom, "approved:")
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("invalid approval count in %q: must be a positive integer", atom)
		}
		return &AtomNode{Atom: Atom{Kind: AtomApproved, ApprovalN: n}}, nil

	case atom == "all-threads-resolved":
		return &AtomNode{Atom: Atom{Kind: AtomAllThreadsResolved}}, nil

	case atom == "ready-for-review":
		return &AtomNode{Atom: Atom{Kind: AtomReadyForReview}}, nil

	case strings.HasPrefix(atom, "label-added:"):
		name := strings.TrimPrefix(atom, "label-added:")
		if name == "" {
			return nil, fmt.Errorf("label name is required in %q", atom)
		}
		return &AtomNode{Atom: Atom{Kind: AtomLabelAdded, LabelName: name}}, nil

	case strings.HasPrefix(atom, "label-removed:"):
		name := strings.TrimPrefix(atom, "label-removed:")
		if name == "" {
			return nil, fmt.Errorf("label name is required in %q", atom)
		}
		return &AtomNode{Atom: Atom{Kind: AtomLabelRemoved, LabelName: name}}, nil

	default:
		m := staleRegexp.FindStringSubmatch(atom)
		if m != nil {
			hours, _ := strconv.Atoi(m[1])
			return &AtomNode{Atom: Atom{
				Kind:          AtomStale,
				StaleDuration: time.Duration(hours) * time.Hour,
			}}, nil
		}
		return nil, fmt.Errorf("unknown trigger atom: %q", atom)
	}
}
