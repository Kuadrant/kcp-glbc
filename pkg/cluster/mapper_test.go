package cluster

import "testing"

func Test_controlContext_Name(t *testing.T) {
	type ctxFields struct {
		name      string
		workspace string
		namespace string
	}
	tests := []struct {
		name      string
		ctxFields ctxFields
		want      string
	}{
		{
			name: "sanitizes context workspace",
			ctxFields: ctxFields{
				workspace: "test:workspace",
				namespace: "testnamespace",
				name:      "testname",
			},
			want: "testworkspace-testnamespace-testname",
		},
		{
			name: "sanitizes context namespace",
			ctxFields: ctxFields{
				workspace: "testworkspace",
				namespace: "test:namespace",
				name:      "testname",
			},
			want: "testworkspace-testnamespace-testname",
		},
		{
			name: "sanitizes context name",
			ctxFields: ctxFields{
				workspace: "testworkspace",
				namespace: "testnamespace",
				name:      "test:name",
			},
			want: "testworkspace-testnamespace-testname",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &context{
				name:      tt.ctxFields.name,
				workspace: tt.ctxFields.workspace,
				namespace: tt.ctxFields.namespace,
			}
			cr := &controlContext{
				context: ctx,
			}
			if got := cr.Name(); got != tt.want {
				t.Errorf("Name() = %v, want %v", got, tt.want)
			}
		})
	}
}
