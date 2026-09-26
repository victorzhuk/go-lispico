package core

import "testing"

func TestNewDialect_RefusalIsDeterministic(t *testing.T) {
	tests := []struct {
		name string
		spec DialectSpec
	}{
		{
			name: "two offending names under one guard",
			spec: DialectSpec{Forms: map[string]string{"a": "nope1", "b": "nope2"}},
		},
		{
			name: "two guards tripped at once",
			spec: DialectSpec{
				Forms:    map[string]string{"a": "nope"},
				Adapters: map[string]Adapter{"f": {Value: Nil{}}},
			},
		},
		{
			name: "two adapters without IDs",
			spec: DialectSpec{Adapters: map[string]Adapter{
				"f": {Value: Nil{}},
				"g": {Value: Nil{}},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewDialect(tt.spec)
			if err == nil {
				t.Fatal("NewDialect accepted the spec; want an error")
			}
			want := err.Error()
			for i := range 20 {
				_, err := NewDialect(tt.spec)
				if err == nil {
					t.Fatalf("call %d: NewDialect accepted the spec; want %q", i, want)
				}
				if got := err.Error(); got != want {
					t.Fatalf("call %d: error = %q, want %q", i, got, want)
				}
			}
		})
	}
}
