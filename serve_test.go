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
		{"", 30 * time.Minute},        // empty falls back
		{"garbage", 30 * time.Minute}, // unparseable falls back
		{"0", 30 * time.Minute},       // non-positive falls back
		{"-5m", 30 * time.Minute},     // negative falls back
	}
	for _, c := range cases {
		if got := parseRefreshInterval(c.in); got != c.want {
			t.Errorf("parseRefreshInterval(%q): got %s want %s", c.in, got, c.want)
		}
	}
}
