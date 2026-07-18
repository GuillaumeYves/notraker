package blocklist

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	input := `
# a comment
0.0.0.0 tracker.example.com
127.0.0.1 pixel.example.net # trailing comment
0.0.0.0 0.0.0.0
0.0.0.0 localhost.localdomain
ads.example.org
localhost
plain-no-dot
`
	got := parse(strings.NewReader(input))
	want := []string{"tracker.example.com", "pixel.example.net", "ads.example.org"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parse() = %v, want %v", got, want)
	}
}

func TestBlockedMatchesSubdomains(t *testing.T) {
	l := New(nil)
	l.Replace([]string{"tracker.example.com"})

	for _, name := range []string{
		"tracker.example.com",
		"tracker.example.com.",
		"TRACKER.example.COM",
		"deep.sub.tracker.example.com",
	} {
		if !l.Blocked(name) {
			t.Errorf("Blocked(%q) = false, want true", name)
		}
	}
	for _, name := range []string{
		"example.com",
		"nottracker.example.com",
		"com",
	} {
		if l.Blocked(name) {
			t.Errorf("Blocked(%q) = true, want false", name)
		}
	}
}
