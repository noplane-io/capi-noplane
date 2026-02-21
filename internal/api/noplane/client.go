package noplane

import (
	"context"
	"fmt"
	"net/http"

	v1 "github.com/noplane-io/capi-noplane/internal/api/noplane/hack/v1"
	"k8s.io/utils/ptr"
)

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
type ClientFactory func(apiKey string) (ClientInterface, error)

// Client is the HTTP client for the noplane.io API.
// TODO: Implement the HTTP methods against https://api.noplane.io/v1.
type Client struct {
	apiKey  string
	baseURL string
	client  *v1.ClientWithResponses
}

// NewClient creates a new noplane.io API client.
func NewClient(apiKey string) (ClientInterface, error) {
	client, err := v1.NewClientWithResponses("https://api.noplane.io/v1",
		v1.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", apiKey)

			return nil
		}))
	if err != nil {
		return nil, err
	}

	return &Client{
		apiKey: apiKey,
		client: client,
	}, nil
}

func (c *Client) CreatePlane(ctx context.Context, req CreateRequest) (*Plane, error) {
	resp, err := c.client.CreateTenantWithResponse(
		ctx, v1.CreateTenantRequest{
			Name:              req.Name,
			UniqueId:          req.Name,
			Subdomain:         &req.Name,
			KubernetesVersion: &req.KubernetesVersion,
			Replicas:          ptr.To(1),
		})
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode() {
	case http.StatusCreated:
		return tenantToPlane(resp.JSON201)
	case http.StatusBadRequest:
		return nil, apiError(resp.StatusCode(), resp.JSON400)
	case http.StatusForbidden:
		return nil, apiError(resp.StatusCode(), resp.JSON403)
	case http.StatusInternalServerError:
		return nil, apiError(resp.StatusCode(), resp.JSON500)
	default:
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode(), string(resp.Body))
	}
}

func (c *Client) GetPlane(ctx context.Context, id string) (*Plane, error) {
	tenants, err := c.listTenants(ctx)
	if err != nil {
		return nil, err
	}

	for i := range tenants {
		if tenants[i].Id == id {
			return tenantToPlane(&tenants[i])
		}
	}
	return nil, fmt.Errorf("plane %q not found", id)
}

func (c *Client) GetPlaneByName(ctx context.Context, name string) (*Plane, error) {
	tenants, err := c.listTenants(ctx)
	if err != nil {
		return nil, err
	}

	for i := range tenants {
		if tenants[i].Name == name {
			return tenantToPlane(&tenants[i])
		}
	}
	return nil, fmt.Errorf("plane with name %q not found", name)
}

func (c *Client) UpdatePlane(_ context.Context, _ string, _ UpdateRequest) error {
	// No update endpoint available in the API yet.
	return fmt.Errorf("UpdatePlane is not supported by the API")
}

func (c *Client) DeletePlane(ctx context.Context, id string) error {
	resp, err := c.client.DeleteTenantWithResponse(ctx, id)
	if err != nil {
		return err
	}

	switch resp.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusBadRequest:
		return apiError(resp.StatusCode(), resp.JSON400)
	case http.StatusForbidden:
		return apiError(resp.StatusCode(), resp.JSON403)
	case http.StatusNotFound:
		return apiError(resp.StatusCode(), resp.JSON404)
	case http.StatusInternalServerError:
		return apiError(resp.StatusCode(), resp.JSON500)
	default:
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode(), string(resp.Body))
	}
}

func (c *Client) GetKubeconfig(ctx context.Context, id string) ([]byte, error) {
	resp, err := c.client.GetTenantKubeconfigWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("empty kubeconfig response")
		}
		return []byte(resp.JSON200.Kubeconfig), nil
	case http.StatusBadRequest:
		return nil, apiError(resp.StatusCode(), resp.JSON400)
	case http.StatusForbidden:
		return nil, apiError(resp.StatusCode(), resp.JSON403)
	case http.StatusNotFound:
		return nil, apiError(resp.StatusCode(), resp.JSON404)
	case http.StatusInternalServerError:
		return nil, apiError(resp.StatusCode(), resp.JSON500)
	default:
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode(), string(resp.Body))
	}
}

// listTenants fetches all tenants from the API.
func (c *Client) listTenants(ctx context.Context) ([]v1.Tenant, error) {
	resp, err := c.client.GetTenantsWithResponse(ctx)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, nil
		}
		return *resp.JSON200, nil
	case http.StatusInternalServerError:
		return nil, apiError(resp.StatusCode(), resp.JSON500)
	default:
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode(), string(resp.Body))
	}
}

// tenantToPlane converts an API Tenant to our internal Plane type.
func tenantToPlane(t *v1.Tenant) (*Plane, error) {
	plane := &Plane{
		ID:     t.Id,
		Name:   t.Name,
		Status: "created",
	}

	if t.KubernetesVersion != nil {
		plane.KubernetesVersion = *t.KubernetesVersion
	}

	switch {
	case t.Hostname != nil:
		plane.Endpoint = Endpoint{
			Host: *t.Hostname,
			Port: 6443,
		}
	case t.Subdomain != nil:
		plane.Endpoint = Endpoint{
			Host: *t.Subdomain + ".k8s.noplane.io",
			Port: 6443,
		}
	}

	return plane, nil
}

// apiError builds an error from an API error response.
func apiError(statusCode int, apiErr *v1.Error) error {
	if apiErr != nil && apiErr.Detail != nil {
		return fmt.Errorf("API error %d: %s", statusCode, *apiErr.Detail)
	}
	return fmt.Errorf("API error %d", statusCode)
}
