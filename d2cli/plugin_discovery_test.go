package d2cli

import "testing"

func TestLayoutFromArgs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		args []string
		want string
	}{
		{name: "fallback", args: []string{"in.d2"}, want: "dagre"},
		{name: "long separate", args: []string{"--layout", "elk", "in.d2"}, want: "elk"},
		{name: "long equals", args: []string{"--layout=tala", "in.d2"}, want: "tala"},
		{name: "short separate", args: []string{"-l", "elk", "in.d2"}, want: "elk"},
		{name: "short equals", args: []string{"-l=tala", "in.d2"}, want: "tala"},
		{name: "short attached", args: []string{"-lelk", "in.d2"}, want: "elk"},
		{name: "last wins", args: []string{"-l", "elk", "--layout=tala", "in.d2"}, want: "tala"},
		{name: "end of flags", args: []string{"--", "--layout=elk"}, want: "dagre"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := layoutFromArgs(tc.args, "dagre"); got != tc.want {
				t.Fatalf("layoutFromArgs(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}
