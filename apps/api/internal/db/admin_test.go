package db

import "testing"

func TestJSONDocumentsEqualComparesNumbersSemanticallyWithoutLosingPrecision(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{
			name:  "PostgreSQL expanded exponent",
			left:  `{"presentation":{"measure":1e+21}}`,
			right: `{"presentation":{"measure":1000000000000000000000}}`,
			want:  true,
		},
		{
			name:  "nested array exponent",
			left:  `{"values":[0.125,3e-2]}`,
			right: `{"values":[0.1250,0.03]}`,
			want:  true,
		},
		{
			name:  "distinct unsafe integers remain distinct",
			left:  `{"value":9007199254740992}`,
			right: `{"value":9007199254740993}`,
			want:  false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := jsonDocumentsEqual([]byte(test.left), []byte(test.right)); got != test.want {
				t.Fatalf("jsonDocumentsEqual(%s, %s) = %t, want %t", test.left, test.right, got, test.want)
			}
		})
	}
}
