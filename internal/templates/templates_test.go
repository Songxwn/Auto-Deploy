package templates

import (
	"strings"
	"testing"

	"github.com/Songxwn/Auto-Deploy/internal/models"
)

func TestBuiltInLibrary(t *testing.T) {
	list := BuiltIn()
	if len(list) == 0 {
		t.Fatal("empty built-in library")
	}
	seen := map[string]bool{}
	var linux, windows int
	for _, tpl := range list {
		if seen[tpl.ID] {
			t.Fatalf("duplicate template id: %s", tpl.ID)
		}
		seen[tpl.ID] = true
		if !tpl.BuiltIn {
			t.Fatalf("%s: built-in flag not set", tpl.ID)
		}
		if strings.TrimSpace(tpl.Name) == "" || strings.TrimSpace(tpl.Script) == "" {
			t.Fatalf("%s: name/script must not be empty", tpl.ID)
		}
		switch tpl.OSType {
		case models.OSLinux:
			linux++
		case models.OSWindows:
			windows++
		default:
			t.Fatalf("%s: invalid os_type %q", tpl.ID, tpl.OSType)
		}
		switch tpl.Category {
		case models.CatWeb, models.CatDatabase, models.CatRuntime, models.CatSystem, models.CatNetwork, models.CatCustom:
		default:
			t.Fatalf("%s: invalid category %q", tpl.ID, tpl.Category)
		}
	}
	if linux == 0 || windows == 0 {
		t.Fatalf("want both OS covered, got linux=%d windows=%d", linux, windows)
	}
}
