package main

import "testing"

func TestTabRunIconName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state tabRunState
		kind  tabKind
		want  string
	}{
		{name: "download idle", state: tabIdle, kind: tabKindDownload, want: "download"},
		{name: "convert idle", state: tabIdle, kind: tabKindConvert, want: "mediaVideo"},
		{name: "running", state: tabRunning, kind: tabKindDownload, want: "viewRefresh"},
		{name: "complete", state: tabComplete, kind: tabKindConvert, want: "confirm"},
		{name: "stopped", state: tabStopped, kind: tabKindDownload, want: "cancel"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := tabRunIconName(test.state, test.kind); got != test.want {
				t.Fatalf("tabRunIconName(%v, %v) = %q, want %q", test.state, test.kind, got, test.want)
			}
		})
	}
}

func TestNumberedSessionTitle(t *testing.T) {
	t.Parallel()

	if got := numberedSessionTitle(tabKindDownload, 1); got != "Download" {
		t.Fatalf("got %q", got)
	}
	if got := numberedSessionTitle(tabKindDownload, 2); got != "Download 2" {
		t.Fatalf("got %q", got)
	}
	if got := numberedSessionTitle(tabKindConvert, 1); got != "Convert" {
		t.Fatalf("got %q", got)
	}
	if got := numberedSessionTitle(tabKindConvert, 3); got != "Convert 3" {
		t.Fatalf("got %q", got)
	}
}

func TestUniqueTitleAddsNumericSuffix(t *testing.T) {
	t.Parallel()

	if got := uniqueTitle("One Piece", nil); got != "One Piece" {
		t.Fatalf("got %q", got)
	}
	if got := uniqueTitle("One Piece", []string{"One Piece"}); got != "One Piece (2)" {
		t.Fatalf("got %q", got)
	}
	if got := uniqueTitle("One Piece", []string{"One Piece", "One Piece (2)"}); got != "One Piece (3)" {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateTitle(t *testing.T) {
	t.Parallel()

	if got := truncateTitle("short", 28); got != "short" {
		t.Fatalf("got %q", got)
	}
	long := "This is a very long series title that should be cut"
	got := truncateTitle(long, 10)
	if got != "This is a…" {
		t.Fatalf("got %q", got)
	}
}
