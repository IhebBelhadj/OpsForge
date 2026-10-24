package terminal

import "testing"

func TestRingKeepsTheLastBytes(t *testing.T) {
	for name, tc := range map[string]struct {
		writes []string
		want   string
	}{
		"empty":                {nil, ""},
		"under size":           {[]string{"ab", "c"}, "abc"},
		"exactly size":         {[]string{"abcd"}, "abcd"},
		"exactly size in two":  {[]string{"ab", "cd"}, "abcd"},
		"wraps":                {[]string{"abc", "de"}, "bcde"},
		"wraps twice":          {[]string{"abc", "def", "gh"}, "efgh"},
		"one write over size":  {[]string{"abcdefg"}, "defg"},
		"over size after wrap": {[]string{"ab", "cdefgh"}, "efgh"},
		"empty write":          {[]string{"ab", ""}, "ab"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRing(4)
			for _, w := range tc.writes {
				r.Write([]byte(w))
			}
			if got := string(r.Bytes()); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
