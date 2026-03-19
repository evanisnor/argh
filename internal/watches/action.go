package watches

import (
	"fmt"
	"strings"
)

// ActionType identifies the kind of a watch action.
type ActionType string

const (
	ActionMerge   ActionType = "merge"
	ActionReady   ActionType = "ready"
	ActionRequest ActionType = "request"
	ActionComment ActionType = "comment"
	ActionLabel   ActionType = "label"
	ActionNotify  ActionType = "notify"
)

// Action is a single watch action parsed from an action expression.
type Action struct {
	Type   ActionType
	Method string // for merge: "squash", "merge", "rebase", or "" for repo default
	User   string // for request: "@user" value
	Text   string // for comment: the comment text
	Name   string // for label: the label name
}

// validMergeMethods lists the allowed merge method values.
var validMergeMethods = map[string]bool{
	"squash": true,
	"merge":  true,
	"rebase": true,
}

// ParseActions parses an action expression into a slice of Action structs.
// Actions can be combined with " + " (with surrounding whitespace trimmed).
// Returns an error for any invalid action in the expression.
func ParseActions(expr string) ([]Action, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("action expression must not be empty")
	}

	parts := strings.Split(expr, "+")
	actions := make([]Action, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty action in expression: %q", expr)
		}
		action, err := parseAction(part)
		if err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}

	return actions, nil
}

// parseAction parses a single action token into an Action struct.
func parseAction(token string) (Action, error) {
	switch {
	case token == "merge":
		return Action{Type: ActionMerge, Method: ""}, nil

	case strings.HasPrefix(token, "merge:"):
		method := strings.TrimPrefix(token, "merge:")
		if !validMergeMethods[method] {
			return Action{}, fmt.Errorf("invalid merge method %q: must be squash, merge, or rebase", method)
		}
		return Action{Type: ActionMerge, Method: method}, nil

	case token == "ready":
		return Action{Type: ActionReady}, nil

	case strings.HasPrefix(token, "request:"):
		user := strings.TrimPrefix(token, "request:")
		if user == "" {
			return Action{}, fmt.Errorf("user is required in %q", token)
		}
		return Action{Type: ActionRequest, User: user}, nil

	case strings.HasPrefix(token, "comment:"):
		text := strings.TrimPrefix(token, "comment:")
		if text == "" {
			return Action{}, fmt.Errorf("text is required in %q", token)
		}
		return Action{Type: ActionComment, Text: text}, nil

	case strings.HasPrefix(token, "label:"):
		name := strings.TrimPrefix(token, "label:")
		if name == "" {
			return Action{}, fmt.Errorf("label name is required in %q", token)
		}
		return Action{Type: ActionLabel, Name: name}, nil

	case token == "notify":
		return Action{Type: ActionNotify}, nil

	default:
		return Action{}, fmt.Errorf("unknown action: %q", token)
	}
}
