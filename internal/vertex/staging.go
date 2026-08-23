package vertex

import (
	"context"
	"fmt"
	"strings"
)

// stagedPackContentType is the media type a staged pack is written with.
const stagedPackContentType = "application/json"

// stagedPackObject is the object name a staged pack is written to.
//
// The pack hash is in the name so re-applying an unchanged pack addresses the
// same object, and a changed pack never overwrites the one a running engine is
// still reading. Engines resolve their pack at startup, so an in-place
// overwrite would hand a restarting engine the new pack while its siblings ran
// the old one.
func stagedPackObject(packHash string) string {
	return fmt.Sprintf("packs/%s/%s", packHash, stagedPackName)
}

// stagedPackURI builds the gs:// URI the runtime fetches.
//
// bucket arrives already validated as gs://-prefixed, so this only has to join
// the two halves without doubling the separator.
func stagedPackURI(bucket, packHash string) string {
	if bucket == "" || packHash == "" {
		return ""
	}
	return strings.TrimRight(bucket, "/") + "/" + stagedPackObject(packHash)
}

// stagePack uploads the pack when it is too large to travel inline, and returns
// the URI the engine should read it from.
//
// An inline pack stages nothing: the bytes ride in the environment and there is
// no object to write.
//
// Re-applying an unchanged pack does not re-upload. The object is keyed by
// hash, so if state already records this exact URI the bytes at the far end are
// the same bytes — checking is a wasted round trip and a wasted write.
func stagePack(
	ctx context.Context, client gcpClient, in *engineInput, prior *State,
) (string, error) {
	if in.Delivery.Inline {
		return "", nil
	}

	// Validation rejects this combination, so reaching it means a caller built
	// an engineInput by hand.
	if in.Cfg.StagingBucket == "" {
		return "", fmt.Errorf(
			"pack is %d bytes, over the inline limit, but no staging_bucket is configured",
			in.Delivery.SizeBytes)
	}

	uri := stagedPackURI(in.Cfg.StagingBucket, in.PackHash)
	if prior != nil && prior.StagedPackURI == uri {
		return uri, nil
	}

	if err := client.StageObject(ctx, uri, []byte(in.PackJSON)); err != nil {
		return "", fmt.Errorf("stage pack to %s: %w", uri, err)
	}
	return uri, nil
}

// validatePackDelivery rejects a pack that cannot be delivered.
//
// This runs at plan time. Without it the plan happily lists a pack_object and
// apply then fails while building the engine spec — the plan promising a
// resource the apply cannot create, which is the specific failure this
// replaces.
func validatePackDelivery(delivery PackDelivery, cfg *Config) error {
	if delivery.Inline || cfg.StagingBucket != "" {
		return nil
	}
	return fmt.Errorf(
		"pack is %d bytes, over the inline limit of %d, so it must be staged to "+
			"Cloud Storage; set staging_bucket",
		delivery.SizeBytes, inlineLimit(cfg))
}

// inlineLimit returns the configured inline limit, or the default.
func inlineLimit(cfg *Config) int {
	if cfg.PackInlineLimitBytes > 0 {
		return cfg.PackInlineLimitBytes
	}
	return DefaultPackInlineLimitBytes
}
