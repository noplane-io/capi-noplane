package fake

import (
	"context"
	"sync"

	"github.com/noplane-io/capi-noplane/internal/api/noplane"
)

var _ noplane.ClientInterface = (*Client)(nil)

// Client implements noplane.ClientInterface for unit tests.
type Client struct {
	mu sync.Mutex

	Planes       map[string]*noplane.Plane // keyed by ID
	PlanesByName map[string]*noplane.Plane // keyed by name
	Kubeconfig   []byte

	CreateErr     error
	GetErr        error
	UpdateErr     error
	DeleteErr     error
	KubeconfigErr error

	CreateCallCount     int
	GetCallCount        int
	GetByNameCallCount  int
	UpdateCallCount     int
	DeleteCallCount     int
	KubeconfigCallCount int
}

// NewClient creates a new fake client with initialized maps.
func NewClient() *Client {
	return &Client{
		Planes:       make(map[string]*noplane.Plane),
		PlanesByName: make(map[string]*noplane.Plane),
		Kubeconfig:   []byte("fake-kubeconfig"),
	}
}

func (f *Client) CreatePlane(_ context.Context, req noplane.CreateRequest) (*noplane.Plane, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CreateCallCount++
	if f.CreateErr != nil {
		return nil, f.CreateErr
	}
	if _, exists := f.PlanesByName[req.Name]; exists {
		return nil, noplane.NewConflictError(req.Name)
	}
	p := &noplane.Plane{
		ID:                "fake-np-id",
		Name:              req.Name,
		Status:            "ready",
		KubernetesVersion: req.KubernetesVersion,
		Endpoint:          noplane.Endpoint{Host: "fake.noplane.io", Port: 6443},
	}
	f.Planes[p.ID] = p
	f.PlanesByName[p.Name] = p
	return p, nil
}

func (f *Client) GetPlane(_ context.Context, id string) (*noplane.Plane, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.GetCallCount++
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	p, ok := f.Planes[id]
	if !ok {
		return nil, noplane.NewNotFoundError(id)
	}
	return p, nil
}

func (f *Client) GetPlaneByName(_ context.Context, name string) (*noplane.Plane, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.GetByNameCallCount++
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	p, ok := f.PlanesByName[name]
	if !ok {
		return nil, noplane.NewNotFoundError(name)
	}
	return p, nil
}

func (f *Client) UpdatePlane(_ context.Context, id string, req noplane.UpdateRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.UpdateCallCount++
	if f.UpdateErr != nil {
		return f.UpdateErr
	}
	p, ok := f.Planes[id]
	if !ok {
		return noplane.NewNotFoundError(id)
	}
	if req.KubernetesVersion != "" {
		p.KubernetesVersion = req.KubernetesVersion
	}
	return nil
}

func (f *Client) DeletePlane(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DeleteCallCount++
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	p, ok := f.Planes[id]
	if !ok {
		return noplane.NewNotFoundError(id)
	}
	delete(f.PlanesByName, p.Name)
	delete(f.Planes, id)
	return nil
}

func (f *Client) GetKubeconfig(_ context.Context, _ string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.KubeconfigCallCount++
	if f.KubeconfigErr != nil {
		return nil, f.KubeconfigErr
	}
	return f.Kubeconfig, nil
}
