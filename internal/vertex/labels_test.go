package vertex

import (
	"strings"
	"testing"
)

func TestSanitizeLabelValue(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercases", "MyPack", "mypack"},
		{"replaces dots", "my.pack", "my-pack"},
		{"replaces slashes", "org/pack", "org-pack"},
		{"keeps hyphens and underscores", "my-pack_v2", "my-pack_v2"},
		{"strips leading separators", "--pack", "pack"},
		{"empty stays empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeLabelValue(tt.in); got != tt.want {
				t.Errorf("sanitizeLabelValue(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeLabelValue_Truncates(t *testing.T) {
	got := sanitizeLabelValue(strings.Repeat("a", 100))
	if len(got) != maxLabelLen {
		t.Errorf("len = %d, want %d", len(got), maxLabelLen)
	}
}

func TestSanitizeLabelValue_IsDeterministic(t *testing.T) {
	in := "Some.Pack/Name"
	first, second := sanitizeLabelValue(in), sanitizeLabelValue(in)
	if first != second {
		t.Error("sanitizeLabelValue is not deterministic")
	}
}

func TestSanitizeLabelKey_MustStartWithLetter(t *testing.T) {
	if got := sanitizeLabelKey("9lives"); !strings.HasPrefix(got, "k") {
		t.Errorf("key starting with a digit should be prefixed, got %q", got)
	}
}

func TestValidateLabels_RejectsCollisions(t *testing.T) {
	labels := map[string]string{
		"My.Team": "a",
		"my-team": "b",
	}

	errs := validateLabels(labels)
	if len(errs) == 0 {
		t.Fatal("expected a collision error")
	}
	if !strings.Contains(strings.Join(errs, "; "), "collide") {
		t.Errorf("error should mention collision, got %v", errs)
	}
}

func TestValidateLabels_RejectsTooMany(t *testing.T) {
	labels := make(map[string]string, maxLabelCount+1)
	for i := 0; i <= maxLabelCount; i++ {
		labels[string(rune('a'+i%26))+strings.Repeat("x", i)] = "v"
	}

	if len(validateLabels(labels)) == 0 {
		t.Error("expected an error for too many labels")
	}
}

func TestValidateLabels_AcceptsClean(t *testing.T) {
	labels := map[string]string{"team": "platform", "env": "prod"}

	if errs := validateLabels(labels); len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

// buildLabels tests land in Phase 1b-ii with the function itself.

func TestManagedLabelKeysAreValid(t *testing.T) {
	for _, key := range []string{LabelPack, LabelAgent, LabelManagedBy} {
		if got := sanitizeLabelKey(key); got != key {
			t.Errorf("managed key %q is not already a valid label key (sanitizes to %q)", key, got)
		}
	}
	if got := sanitizeLabelValue(ManagedByValue); got != ManagedByValue {
		t.Errorf("ManagedByValue %q is not a valid label value (sanitizes to %q)",
			ManagedByValue, got)
	}
}
