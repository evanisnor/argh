package watches

import (
	"testing"
)

func TestParseActions(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		want    []Action
		wantErr bool
	}{
		// --- single actions ---
		{
			name: "merge no method",
			expr: "merge",
			want: []Action{{Type: ActionMerge, Method: ""}},
		},
		{
			name: "merge squash",
			expr: "merge:squash",
			want: []Action{{Type: ActionMerge, Method: "squash"}},
		},
		{
			name: "merge merge",
			expr: "merge:merge",
			want: []Action{{Type: ActionMerge, Method: "merge"}},
		},
		{
			name: "merge rebase",
			expr: "merge:rebase",
			want: []Action{{Type: ActionMerge, Method: "rebase"}},
		},
		{
			name: "ready",
			expr: "ready",
			want: []Action{{Type: ActionReady}},
		},
		{
			name: "request user",
			expr: "request:@alice",
			want: []Action{{Type: ActionRequest, User: "@alice"}},
		},
		{
			name: "review single user",
			expr: "review:alice",
			want: []Action{{Type: ActionReview, Users: []string{"alice"}}},
		},
		{
			name: "review multiple users",
			expr: "review:alice,bob,carol",
			want: []Action{{Type: ActionReview, Users: []string{"alice", "bob", "carol"}}},
		},
		{
			name: "comment with text",
			expr: "comment:this is text",
			want: []Action{{Type: ActionComment, Text: "this is text"}},
		},
		{
			name: "label name",
			expr: "label:bug",
			want: []Action{{Type: ActionLabel, Name: "bug"}},
		},
		{
			name: "notify",
			expr: "notify",
			want: []Action{{Type: ActionNotify}},
		},

		// --- combined actions ---
		{
			name: "comment plus notify",
			expr: "comment:text + notify",
			want: []Action{
				{Type: ActionComment, Text: "text"},
				{Type: ActionNotify},
			},
		},
		{
			name: "merge:squash plus notify",
			expr: "merge:squash + notify",
			want: []Action{
				{Type: ActionMerge, Method: "squash"},
				{Type: ActionNotify},
			},
		},
		{
			name: "three combined actions",
			expr: "merge:squash + comment:done + notify",
			want: []Action{
				{Type: ActionMerge, Method: "squash"},
				{Type: ActionComment, Text: "done"},
				{Type: ActionNotify},
			},
		},
		{
			name: "request plus notify",
			expr: "request:@bob + notify",
			want: []Action{
				{Type: ActionRequest, User: "@bob"},
				{Type: ActionNotify},
			},
		},
		{
			name: "review plus notify",
			expr: "review:alice,bob + notify",
			want: []Action{
				{Type: ActionReview, Users: []string{"alice", "bob"}},
				{Type: ActionNotify},
			},
		},
		{
			name: "label plus notify",
			expr: "label:wip + notify",
			want: []Action{
				{Type: ActionLabel, Name: "wip"},
				{Type: ActionNotify},
			},
		},

		// --- whitespace handling ---
		{
			name: "leading and trailing whitespace",
			expr: "  notify  ",
			want: []Action{{Type: ActionNotify}},
		},

		// --- error cases ---
		{
			name:    "empty expression",
			expr:    "",
			wantErr: true,
		},
		{
			name:    "unknown action type",
			expr:    "bogus",
			wantErr: true,
		},
		{
			name:    "merge with invalid method",
			expr:    "merge:fast-forward",
			wantErr: true,
		},
		{
			name:    "request without user",
			expr:    "request:",
			wantErr: true,
		},
		{
			name:    "review without users",
			expr:    "review:",
			wantErr: true,
		},
		{
			name:    "review with empty user segment",
			expr:    "review:alice,,bob",
			wantErr: true,
		},
		{
			name:    "comment without text",
			expr:    "comment:",
			wantErr: true,
		},
		{
			name:    "label without name",
			expr:    "label:",
			wantErr: true,
		},
		{
			name:    "empty action segment after plus",
			expr:    "notify + ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseActions(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseActions(%q) error = %v, wantErr %v", tt.expr, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ParseActions(%q) returned %d actions, want %d: got %+v", tt.expr, len(got), len(tt.want), got)
			}
			for i, a := range got {
				w := tt.want[i]
				if a.Type != w.Type {
					t.Errorf("action[%d].Type = %q, want %q", i, a.Type, w.Type)
				}
				if a.Method != w.Method {
					t.Errorf("action[%d].Method = %q, want %q", i, a.Method, w.Method)
				}
				if a.User != w.User {
					t.Errorf("action[%d].User = %q, want %q", i, a.User, w.User)
				}
				if a.Text != w.Text {
					t.Errorf("action[%d].Text = %q, want %q", i, a.Text, w.Text)
				}
				if a.Name != w.Name {
					t.Errorf("action[%d].Name = %q, want %q", i, a.Name, w.Name)
				}
				if len(a.Users) != len(w.Users) {
					t.Errorf("action[%d].Users = %v, want %v", i, a.Users, w.Users)
				} else {
					for j := range a.Users {
						if a.Users[j] != w.Users[j] {
							t.Errorf("action[%d].Users[%d] = %q, want %q", i, j, a.Users[j], w.Users[j])
						}
					}
				}
			}
		})
	}
}
