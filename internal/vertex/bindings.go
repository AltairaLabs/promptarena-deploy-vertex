package vertex

// ProviderBinding maps a logical provider name to a concrete provider, either
// by referencing an arena provider or by declaring type and model inline.
type ProviderBinding struct {
	Name           string `json:"name"`
	Role           string `json:"role,omitempty"`
	ArenaProvider  string `json:"arena_provider,omitempty"`
	Type           string `json:"type,omitempty"`
	Model          string `json:"model,omitempty"`
	VertexEndpoint string `json:"vertex_endpoint,omitempty"`
}
