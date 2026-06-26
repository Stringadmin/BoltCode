package command

import "testing"

func TestFileOverlap(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want int
	}{
		{"no overlap", []string{"a.go", "b.go"}, []string{"c.go", "d.go"}, 0},
		{"one overlap", []string{"a.go", "b.go"}, []string{"b.go", "c.go"}, 1},
		{"full overlap", []string{"a.go", "b.go"}, []string{"a.go", "b.go"}, 2},
		{"empty a", nil, []string{"a.go"}, 0},
		{"empty b", []string{"a.go"}, nil, 0},
		{"both empty", nil, nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fileOverlap(tt.a, tt.b)
			if len(got) != tt.want {
				t.Errorf("fileOverlap(%v, %v) = %v (len %d), want len %d",
					tt.a, tt.b, got, len(got), tt.want)
			}
		})
	}
}
