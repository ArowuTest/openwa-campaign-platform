package provider

import "testing"

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		actual, minimum string
		want            bool
	}{
		{"0.13.0", "0.13.0", true},
		{"0.13.1", "0.13.0", true},
		{"1.0.0", "0.13.99", true},
		{"0.12.9", "0.13.0", false},
		{"v0.13.0+platform.2", "0.13.0", true},
		{"0.13.0-rc.2", "0.13.0", false},
		{"0.13.0", "0.13.0-rc.2", true},
		{"0.13.0-rc.10", "0.13.0-rc.2", true},
	}
	for _, tc := range cases {
		got, err := VersionAtLeast(tc.actual, tc.minimum)
		if err != nil {
			t.Fatalf("VersionAtLeast(%q,%q): %v", tc.actual, tc.minimum, err)
		}
		if got != tc.want {
			t.Fatalf("VersionAtLeast(%q,%q)=%v, want %v", tc.actual, tc.minimum, got, tc.want)
		}
	}
}

func TestVersionAtLeastRejectsInvalidInput(t *testing.T) {
	for _, value := range []string{"", "1..2", "1.02.3", "one.two.three", "1.2.3-"} {
		if _, err := VersionAtLeast(value, "0.13.0"); err == nil {
			t.Fatalf("expected invalid actual version %q to fail", value)
		}
	}
}
