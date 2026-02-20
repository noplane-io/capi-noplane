package noplane

import "context"

// ClientInterface defines the operations against the noplane.io API.
type ClientInterface interface {
	CreatePlane(ctx context.Context, req CreateRequest) (*Plane, error)
	GetPlane(ctx context.Context, id string) (*Plane, error)
	GetPlaneByName(ctx context.Context, name string) (*Plane, error)
	UpdatePlane(ctx context.Context, id string, req UpdateRequest) error
	DeletePlane(ctx context.Context, id string) error
	GetKubeconfig(ctx context.Context, id string) ([]byte, error)
}

// ClientFactory creates a ClientInterface given an API key.
// Injected into the reconciler to allow per-reconcile credential resolution.
type ClientFactory func(apiKey string) ClientInterface

// Client is the HTTP client for the noplane.io API.
// TODO: Implement the HTTP methods against https://api.noplane.io/v1.
type Client struct {
	apiKey  string
	baseURL string
}

// NewClient creates a new noplane.io API client.
func NewClient(apiKey string) ClientInterface {
	return &Client{
		apiKey:  apiKey,
		baseURL: "https://api.noplane.io/v1",
	}
}

func (c *Client) CreatePlane(_ context.Context, _ CreateRequest) (*Plane, error) {
	panic("not implemented: CreatePlane")
}

func (c *Client) GetPlane(_ context.Context, _ string) (*Plane, error) {
	panic("not implemented: GetPlane")
}

func (c *Client) GetPlaneByName(_ context.Context, _ string) (*Plane, error) {
	panic("not implemented: GetPlaneByName")
}

func (c *Client) UpdatePlane(_ context.Context, _ string, _ UpdateRequest) error {
	panic("not implemented: UpdatePlane")
}

func (c *Client) DeletePlane(_ context.Context, _ string) error {
	panic("not implemented: DeletePlane")
}

func (c *Client) GetKubeconfig(_ context.Context, _ string) ([]byte, error) {
	panic("not implemented: GetKubeconfig")
}
