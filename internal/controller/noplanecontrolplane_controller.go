/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	controlplanev1alpha1 "github.com/noplane-io/capi-noplane/api/v1alpha1"
	"github.com/noplane-io/capi-noplane/internal/api/noplane"
)

const (
	noplaneFinalizer = "controlplane.noplane.io/finalizer"
	fieldOwner       = "noplane-controlplane-controller"
)

// NoPlaneControlPlaneReconciler reconciles a NoPlaneControlPlane object.
type NoPlaneControlPlaneReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	ClientFactory noplane.ClientFactory
}

// +kubebuilder:rbac:groups=controlplane.noplane.io,resources=noplanecontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.noplane.io,resources=noplanecontrolplanes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=controlplane.noplane.io,resources=noplanecontrolplanes/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *NoPlaneControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var ncp controlplanev1alpha1.NoPlaneControlPlane
	if err := r.Get(ctx, req.NamespacedName, &ncp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Wait for CAPI core to set the ownerReference before acting.
	cluster, err := util.GetOwnerCluster(ctx, r.Client, ncp.ObjectMeta)
	if err != nil || cluster == nil {
		return ctrl.Result{}, err
	}

	// Respect pause annotation on either the Cluster or the NoPlaneControlPlane.
	if annotations.IsPaused(cluster, &ncp) {
		log.Info("paused, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	// Add finalizer on first reconcile.
	if !controllerutil.ContainsFinalizer(&ncp, noplaneFinalizer) {
		controllerutil.AddFinalizer(&ncp, noplaneFinalizer)
		return ctrl.Result{}, r.Update(ctx, &ncp)
	}

	// Resolve API key and create client.
	apiKey, err := r.getAPIKey(ctx, ncp.Spec.CredentialsSecretRef)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("fetching API key: %w", err)
	}
	apiClient, err := r.ClientFactory(apiKey)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("creating API client: %w", err)
	}

	if !ncp.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &ncp, apiClient)
	}

	return r.reconcileNormal(ctx, cluster, &ncp, apiClient)
}

func (r *NoPlaneControlPlaneReconciler) reconcileNormal(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	ncp *controlplanev1alpha1.NoPlaneControlPlane,
	apiClient noplane.ClientInterface,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	planeID, err := r.resolvePlaneID(ctx, ncp, apiClient)
	if err != nil {
		return ctrl.Result{}, err
	}

	plane, err := apiClient.GetPlane(ctx, planeID)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("getting plane: %w", err)
	}

	if plane.Status != "ready" {
		log.Info("plane not yet ready", "status", plane.Status)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	// Trigger upgrade if spec version drifts from actual.
	if plane.KubernetesVersion != ncp.Spec.Version {
		if err := apiClient.UpdatePlane(ctx, planeID, noplane.UpdateRequest{
			KubernetesVersion: ncp.Spec.Version,
		}); err != nil {
			return ctrl.Result{}, fmt.Errorf("upgrading plane: %w", err)
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Surface endpoint for CAPI core.
	ncp.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
		Host: plane.Endpoint.Host,
		Port: int32(plane.Endpoint.Port),
	}

	if err := r.reconcileKubeconfig(ctx, cluster, ncp, apiClient); err != nil {
		return ctrl.Result{}, err
	}

	// Update status.
	ncp.Status.PlaneID = planeID
	ncp.Status.Ready = true
	ncp.Status.Initialized = true // never set back to false once true
	ncp.Status.Version = plane.KubernetesVersion

	return ctrl.Result{RequeueAfter: 60 * time.Second}, r.Status().Update(ctx, ncp)
}

// resolvePlaneID returns the noplane.io plane ID, creating the control plane
// if it does not yet exist. It never creates a duplicate.
//
// Resolution order:
//  1. spec.planeID set by user     → brownfield adoption, skip creation
//  2. status.planeID persisted     → crash recovery, reuse existing ID
//  3. Neither set                  → attempt creation:
//     409 Conflict → look up by name and adopt
//     success      → persist ID to status immediately
func (r *NoPlaneControlPlaneReconciler) resolvePlaneID(
	ctx context.Context,
	ncp *controlplanev1alpha1.NoPlaneControlPlane,
	apiClient noplane.ClientInterface,
) (string, error) {
	log := logf.FromContext(ctx)

	// 1. Brownfield: user explicitly provided the ID in spec.
	if ncp.Spec.PlaneID != "" {
		log.Info("adopting existing plane from spec.planeID", "id", ncp.Spec.PlaneID)
		return ncp.Spec.PlaneID, nil
	}

	// 2. Crash recovery: ID was already persisted to status.
	if ncp.Status.PlaneID != "" {
		log.Info("resuming existing plane from status.planeID", "id", ncp.Status.PlaneID)
		return ncp.Status.PlaneID, nil
	}

	// 3. First reconcile: attempt creation.
	plane, err := apiClient.CreatePlane(ctx, noplane.CreateRequest{
		Name:              ncp.Name,
		KubernetesVersion: ncp.Spec.Version,
	})
	if err != nil {
		if !noplane.IsConflict(err) {
			return "", fmt.Errorf("creating plane: %w", err)
		}
		// 409: plane with this name already exists — look it up.
		log.Info("409 on create, looking up existing plane by name", "name", ncp.Name)
		plane, err = apiClient.GetPlaneByName(ctx, ncp.Name)
		if err != nil {
			return "", fmt.Errorf("looking up plane by name after 409: %w", err)
		}
	}

	// Persist the ID immediately so a subsequent crash takes path 2.
	ncp.Status.PlaneID = plane.ID
	if err := r.Status().Update(ctx, ncp); err != nil {
		return "", fmt.Errorf("persisting planeID to status: %w", err)
	}

	return plane.ID, nil
}

func (r *NoPlaneControlPlaneReconciler) reconcileDelete(
	ctx context.Context,
	ncp *controlplanev1alpha1.NoPlaneControlPlane,
	apiClient noplane.ClientInterface,
) (ctrl.Result, error) {
	if ncp.Status.PlaneID == "" {
		// Nothing was ever created; remove finalizer.
		controllerutil.RemoveFinalizer(ncp, noplaneFinalizer)
		return ctrl.Result{}, r.Update(ctx, ncp)
	}

	if err := apiClient.DeletePlane(ctx, ncp.Status.PlaneID); err != nil {
		if !noplane.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("deleting plane: %w", err)
		}
		// Already gone — treat as success.
	}

	controllerutil.RemoveFinalizer(ncp, noplaneFinalizer)
	return ctrl.Result{}, r.Update(ctx, ncp)
}

func (r *NoPlaneControlPlaneReconciler) reconcileKubeconfig(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	ncp *controlplanev1alpha1.NoPlaneControlPlane,
	apiClient noplane.ClientInterface,
) error {
	kubeconfigBytes, err := apiClient.GetKubeconfig(ctx, ncp.Status.PlaneID)
	if err != nil {
		return fmt.Errorf("fetching kubeconfig from noplane.io: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-kubeconfig", cluster.Name),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(cluster, clusterv1.GroupVersion.WithKind("Cluster")),
			},
		},
		Data: map[string][]byte{
			"value": kubeconfigBytes,
		},
	}

	existing := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, secret)
	}
	if err != nil {
		return fmt.Errorf("checking kubeconfig secret: %w", err)
	}

	existing.Data = secret.Data
	existing.Labels = secret.Labels
	return r.Update(ctx, existing)
}

func (r *NoPlaneControlPlaneReconciler) getAPIKey(ctx context.Context, ref corev1.SecretReference) (string, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      ref.Name,
		Namespace: ref.Namespace,
	}, secret); err != nil {
		return "", fmt.Errorf("getting credentials secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}

	apiKey, ok := secret.Data["apiKey"]
	if !ok {
		return "", fmt.Errorf("credentials secret %s/%s missing 'apiKey' key", ref.Namespace, ref.Name)
	}

	return string(apiKey), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NoPlaneControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1alpha1.NoPlaneControlPlane{}).
		Named("noplanecontrolplane").
		Complete(r)
}
