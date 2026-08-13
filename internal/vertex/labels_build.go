package vertex

// managedLabelCount is the number of promptkit-* labels buildLabels adds.
const managedLabelCount = 3

// buildLabels merges sanitized user labels with the managed labels. Managed
// labels always win, so a user cannot break state recovery by overriding them.
func buildLabels(cfg *Config, packID, agentName string) (built map[string]string, errors []string) {
	if errs := validateLabels(cfg.Labels); len(errs) != 0 {
		return nil, errs
	}

	out := make(map[string]string, len(cfg.Labels)+managedLabelCount)
	for k, v := range cfg.Labels {
		out[sanitizeLabelKey(k)] = sanitizeLabelValue(v)
	}

	out[LabelPack] = sanitizeLabelValue(packID)
	out[LabelAgent] = sanitizeLabelValue(agentName)
	out[LabelManagedBy] = ManagedByValue

	return out, nil
}
