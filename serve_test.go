package main

import (
	"testing"
	"time"
)

func TestParseRefreshInterval(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"15m", 15 * time.Minute},
		{"1h", time.Hour},
		{"", defaultRefreshInterval},        // empty falls back
		{"garbage", defaultRefreshInterval}, // unparseable falls back
		{"0", defaultRefreshInterval},       // non-positive falls back
		{"-5m", defaultRefreshInterval},     // negative falls back
	}
	for _, c := range cases {
		if got := parseRefreshInterval(c.in); got != c.want {
			t.Errorf("parseRefreshInterval(%q): got %s want %s", c.in, got, c.want)
		}
	}
}
