package hosts

import (
	"reflect"
	"testing"
)

func TestNormalizeProxyIDs(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{nil, nil},
		{[]string{"", "  "}, nil},
		{[]string{" prx_a ", "prx_a", "direct"}, []string{"prx_a", "direct"}},
		{[]string{"a", "b", "c"}, []string{"a", "b"}},
	}
	for _, c := range cases {
		if got := NormalizeProxyIDs(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("NormalizeProxyIDs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
