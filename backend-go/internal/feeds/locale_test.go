package feeds

import (
	"reflect"
	"testing"
)

func TestEveryFeedLocaleHasEveryMessage(t *testing.T) {
	for _, locale := range []Locale{English, Japanese, Chinese} {
		m, ok := catalog[locale]
		if !ok {
			t.Fatalf("%s has no messages", locale)
		}
		value := reflect.ValueOf(m)
		for i := range value.NumField() {
			if value.Field(i).String() == "" {
				t.Fatalf("%s lacks %s", locale, value.Type().Field(i).Name)
			}
		}
	}
	for raw, want := range map[string]Locale{"": English, "en": English, "ja": Japanese, " zh-CN ": Chinese, "zh": English, "JA": English} {
		if got := ParseLocale(raw); got != want {
			t.Fatalf("ParseLocale(%q)=%s, want %s", raw, got, want)
		}
	}
}
