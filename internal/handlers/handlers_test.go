package handlers

import "testing"

func TestNormalizeDigestKind(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"", "news", true},
		{"news", "news", true},
		{"facts", "facts", true},
		{"News", "", false},
		{"NEWS", "", false},
		{"unknown", "", false},
		{" news ", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeDigestKind(c.in)
		if got != c.want || ok != c.wantOK {
			t.Errorf("normalizeDigestKind(%q) = (%q, %v), want (%q, %v)",
				c.in, got, ok, c.want, c.wantOK)
		}
	}
}
