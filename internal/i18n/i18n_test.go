package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestCatalogsMatch(t *testing.T) {
	for key := range catalog[En] {
		if _, ok := catalog[ZhCN][key]; !ok {
			t.Errorf("zh-CN lacks %q", key)
		}
	}
	for key := range catalog[ZhCN] {
		if _, ok := catalog[En][key]; !ok {
			t.Errorf("en lacks %q", key)
		}
	}
}

// TestEveryRuleHasAMessage scans the detection code for rule ids so a new
// rule cannot ship without a translation.
var ruleID = regexp.MustCompile(`"((?:meta|class|mixin|res|nested|fallback|modrinth|curseforge|pack|deps)\.[A-Za-z.]+)"`)

func TestEveryRuleHasAMessage(t *testing.T) {
	files, _ := filepath.Glob("../detect/*.go")
	more, _ := filepath.Glob("../scan/*.go")
	seen := 0
	for _, f := range append(files, more...) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range ruleID.FindAllStringSubmatch(string(src), -1) {
			key := m[1]
			if key == "deps.requiredBy." {
				continue // completed with the side at runtime
			}
			seen++
			if _, ok := catalog[En][key]; !ok {
				t.Errorf("%s: rule %q has no message", filepath.Base(f), key)
			}
		}
	}
	if seen < 20 {
		t.Errorf("found only %d rule ids; is the pattern stale?", seen)
	}
}

func TestDetect(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "zh_CN.UTF-8")
	if Detect("") != ZhCN || Detect("en") != En || Detect("zh") != ZhCN {
		t.Error("language detection")
	}
	if got := En.T("class.mostlyClient", "3", "4"); got != "3 of 4 game classes are client code" {
		t.Errorf("T = %q", got)
	}
}
