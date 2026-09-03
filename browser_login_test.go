package main

import (
	"testing"

	"github.com/chromedp/cdproto/network"
)

func TestEtpRtFromCookies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cookies []*network.Cookie
		want    string
		found   bool
	}{
		{
			name: "finds session cookie",
			cookies: []*network.Cookie{
				{Name: "other", Value: "ignored"},
				{Name: "etp_rt", Value: "session-value"},
			},
			want:  "session-value",
			found: true,
		},
		{
			name: "ignores empty session cookie",
			cookies: []*network.Cookie{
				{Name: "etp_rt", Value: ""},
			},
		},
		{
			name:    "handles nil entries",
			cookies: []*network.Cookie{nil},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, found := etpRtFromCookies(test.cookies)
			if got != test.want {
				t.Fatalf("etpRtFromCookies() cookie = %q, want %q", got, test.want)
			}
			if found != test.found {
				t.Fatalf("etpRtFromCookies() found = %t, want %t", found, test.found)
			}
		})
	}
}
