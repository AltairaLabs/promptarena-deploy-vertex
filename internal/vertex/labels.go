package vertex

import (
	"fmt"
	"regexp"
	"strings"
)

// GCP label constraints: keys and values are lowercase letters, digits,
// hyphens and underscores, at most 63 characters; keys must start with a
// lowercase letter; at most 64 labels per resource.
const (
	maxLabelLen   = 63
	maxLabelCount = 64
)

// Managed label keys applied to every resource this adapter creates. They make
// state recoverable: engines can be relisted by pack and agent when the state
// file is lost.
const (
	LabelPack      = "promptkit-pack"
	LabelAgent     = "promptkit-agent"
	LabelManagedBy = "promptkit-managed-by"
)

// ManagedByValue is the LabelManagedBy value identifying resources this
// adapter owns. Listing engines by this label is how state is recovered when
// the state file is lost.
const ManagedByValue = "promptarena-deploy-vertex"

// labelDisallowed matches any character not permitted in a label.
var labelDisallowed = regexp.MustCompile(`[^a-z0-9_-]+`)

// labelLeading matches leading characters that are not lowercase letters.
var labelLeading = regexp.MustCompile(`^[^a-z]+`)

// sanitizeLabelValue converts an arbitrary string into a valid GCP label value.
// The mapping is deterministic: the same input always yields the same output.
func sanitizeLabelValue(s string) string {
	if s == "" {
		return ""
	}
	out := labelDisallowed.ReplaceAllString(strings.ToLower(s), "-")
	out = strings.Trim(out, "-_")
	if len(out) > maxLabelLen {
		out = out[:maxLabelLen]
		out = strings.Trim(out, "-_")
	}
	return out
}

// sanitizeLabelKey converts an arbitrary string into a valid GCP label key.
// Keys must start with a lowercase letter, so a leading digit or separator is
// given a "k" prefix rather than stripped, which would risk collisions.
func sanitizeLabelKey(s string) string {
	out := sanitizeLabelValue(s)
	if out == "" {
		return ""
	}
	if labelLeading.MatchString(out) {
		out = "k" + out
		if len(out) > maxLabelLen {
			out = out[:maxLabelLen]
		}
	}
	return out
}

// validateLabels checks user labels for count, emptiness and sanitization
// collisions. Two distinct keys that sanitize to the same value are rejected
// rather than silently merged.
func validateLabels(labels map[string]string) []string {
	if len(labels) == 0 {
		return nil
	}

	var errs []string
	if len(labels) > maxLabelCount {
		errs = append(errs, fmt.Sprintf(
			"labels: at most %d labels allowed, got %d", maxLabelCount, len(labels)))
	}

	origin := make(map[string]string, len(labels))
	for k := range labels {
		sanitized := sanitizeLabelKey(k)
		if sanitized == "" {
			errs = append(errs, fmt.Sprintf(
				"labels: key %q cannot be sanitized to a valid GCP label key", k))
			continue
		}
		if prev, ok := origin[sanitized]; ok {
			errs = append(errs, fmt.Sprintf(
				"labels: keys %q and %q collide as %q after sanitization", prev, k, sanitized))
			continue
		}
		origin[sanitized] = k
	}

	return errs
}

// buildLabels — merging sanitized user labels with the managed labels — lands
// in Phase 1b-ii alongside Apply, which is the first caller that creates
// resources to label. The managed key constants above are defined here so the
// label rules stay in one file.
