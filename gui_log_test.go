//go:build gui

package main

import "testing"

func TestRenderLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "empty input renders nothing",
			raw:  "",
			want: "",
		},
		{
			name: "single line is unchanged",
			raw:  "Downloading: The End of Adventure\n",
			want: "Downloading: The End of Adventure",
		},
		{
			name: "newest line comes first",
			raw:  "first\nsecond\nthird\n",
			want: "third\nsecond\nfirst",
		},
		{
			name: "trailing newlines do not become blank leading lines",
			raw:  "first\nsecond\n\n\n",
			want: "second\nfirst",
		},
		{
			name: "missing trailing newline still renders",
			raw:  "first\nsecond",
			want: "second\nfirst",
		},
		{
			name: "carriage returns collapse to the latest progress",
			raw:  "Downloaded 1 of 3 segments\rDownloaded 3 of 3 segments\nFinished!\n",
			want: "Finished!\nDownloaded 3 of 3 segments",
		},
		{
			name: "interior blank lines are preserved",
			raw:  "first\n\nthird\n",
			want: "third\n\nfirst",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := renderLog(test.raw); got != test.want {
				t.Fatalf("renderLog(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}
