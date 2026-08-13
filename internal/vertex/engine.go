package vertex

import (
	"encoding/json"
	"fmt"
)

// Environment variable names injected into the runtime container. These must
// match cmd/vertex-runtime/config.go exactly.
const (
	envPackJSON  = "PROMPTPACK_PACK_JSON"
	envPackURI   = "PROMPTPACK_PACK_URI"
	envAgentName = "PROMPTPACK_AGENT"
	envProviders = "PROMPTPACK_PROVIDERS"
	envProject   = "GOOGLE_CLOUD_PROJECT"
	envLocation  = "GOOGLE_CLOUD_LOCATION"
)

// engineInput is everything buildEngine needs, gathered once by Apply and
// reused for every agent in the pack.
type engineInput struct {
	Cfg           *Config
	PackJSON      string
	PackID        string
	PackHash      string
	ConfigHash    string
	Bindings      []ResolvedBinding
	Delivery      PackDelivery
	StagedPackURI string
	AgentCards    map[string]map[string]any
}

// buildEngine turns the deployment inputs into the desired spec for one agent.
// It is pure: no I/O, so the whole mapping is testable offline.
func buildEngine(in *engineInput, agent AgentInfo) (built *EngineSpec, errors []string) {
	env, errs := buildEngineEnv(in, agent.Name)
	if len(errs) != 0 {
		return nil, errs
	}

	labels, labelErrs := buildLabels(in.Cfg, in.PackID, agent.Name)
	if len(labelErrs) != 0 {
		return nil, labelErrs
	}

	spec := &EngineSpec{
		DisplayName:          agent.Name,
		Description:          fmt.Sprintf("PromptKit agent %q from pack %q", agent.Name, in.PackID),
		ImageURI:             in.Cfg.Image,
		ServiceAccount:       in.Cfg.ServiceAccount,
		Labels:               labels,
		Env:                  env,
		ResourceLimits:       resourceLimitsMap(in.Cfg.ResourceLimits),
		MinInstances:         in.Cfg.MinInstances,
		MaxInstances:         in.Cfg.MaxInstances,
		ContainerConcurrency: in.Cfg.ContainerConcurrency,
		AgentCard:            in.AgentCards[agent.Name],
	}

	return spec, nil
}

// buildEngineEnv assembles the runtime's environment.
func buildEngineEnv(in *engineInput, agentName string) (vars map[string]string, errors []string) {
	encodedBindings, err := json.Marshal(in.Bindings)
	if err != nil {
		return nil, []string{fmt.Sprintf("encode provider bindings: %v", err)}
	}

	env := map[string]string{
		envAgentName: agentName,
		envProviders: string(encodedBindings),
		envProject:   in.Cfg.Project,
		envLocation:  in.Cfg.Location,
	}

	if in.Delivery.Inline {
		env[envPackJSON] = in.PackJSON
		return env, nil
	}

	if in.StagedPackURI == "" {
		return nil, []string{
			"pack exceeds the inline limit but no staged pack URI is available",
		}
	}
	env[envPackURI] = in.StagedPackURI
	return env, nil
}

// resourceLimitsMap converts the config's limits into the cpu/memory map the
// API accepts. Nil stays nil so the API's own default applies.
func resourceLimitsMap(limits *ResourceLimits) map[string]string {
	if limits == nil {
		return nil
	}
	out := map[string]string{}
	if limits.CPU != "" {
		out["cpu"] = limits.CPU
	}
	if limits.Memory != "" {
		out["memory"] = limits.Memory
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
