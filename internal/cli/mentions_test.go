package cli

import (
	"reflect"
	"testing"
)

func TestParseMentions(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"no mentions here", nil},
		{"@reviewer 見て", []string{"reviewer"}},
		{"@a and @b and @a again", []string{"a", "b"}},
		{"trailing punct @bob, @carol!", []string{"bob", "carol"}},
		{"email a@b.com is not a mention", nil},
		{"@ alone", nil},
		{"@under_score-hyphen ok", []string{"under_score-hyphen"}},
	}
	for _, c := range cases {
		got := parseMentions(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseMentions(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
