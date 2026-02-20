package noplane

// Plane represents a hosted control plane as returned by the noplane.io API.
type Plane struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Status            string   `json:"status"`
	Endpoint          Endpoint `json:"endpoint"`
	KubernetesVersion string   `json:"kubernetesVersion"`
}

// Endpoint represents the API server endpoint of a control plane.
type Endpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// CreateRequest is the payload for POST /planes.
type CreateRequest struct {
	Name              string `json:"name"`
	KubernetesVersion string `json:"kubernetesVersion"`
}

// UpdateRequest is the payload for PATCH /planes/{id}.
type UpdateRequest struct {
	KubernetesVersion string `json:"kubernetesVersion,omitempty"`
}
