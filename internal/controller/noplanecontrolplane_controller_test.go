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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	controlplanev1alpha1 "github.com/noplane-io/capi-noplane/api/v1alpha1"
	"github.com/noplane-io/capi-noplane/internal/api/noplane"
	fakeclient "github.com/noplane-io/capi-noplane/internal/api/noplane/fake"
)

var _ = Describe("NoPlaneControlPlane Controller", func() {

	const ns = "default"

	Context("Reconcile entry paths", func() {
		It("should return nil when the resource does not exist", func() {
			fakeAPI := fakeclient.NewClient()
			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return nil when there is no owner cluster", func() {
			ncp := &controlplanev1alpha1.NoPlaneControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-owner-ncp",
					Namespace: ns,
				},
				Spec: controlplanev1alpha1.NoPlaneControlPlaneSpec{
					Version: "v1.29.2",
					CredentialsSecretRef: corev1.SecretReference{
						Name: "no-owner-creds", Namespace: ns,
					},
				},
			}
			Expect(k8sClient.Create(ctx, ncp)).To(Succeed())
			defer cleanupObjects(ctx, ncp)

			fakeAPI := fakeclient.NewClient()
			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeAPI.CreateCallCount).To(Equal(0))
		})

		It("should skip reconciliation when the cluster is paused", func() {
			cluster := makeCluster(ctx, "paused-cluster")
			cluster.Spec.Paused = ptr.To(true)
			Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

			ncp := makeNCP(ctx, "paused-ncp", cluster, "paused-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "paused-creds")
			defer cleanupObjects(ctx, cluster, ncp)

			fakeAPI := fakeclient.NewClient()
			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeAPI.CreateCallCount).To(Equal(0))
		})

		It("should skip reconciliation when the NCP has the paused annotation", func() {
			cluster := makeCluster(ctx, "ncp-paused-cluster")
			ncp := makeNCP(ctx, "ncp-paused-ncp", cluster, "ncp-paused-creds", func(n *controlplanev1alpha1.NoPlaneControlPlane) {
				n.Annotations = map[string]string{
					clusterv1.PausedAnnotation: "true",
				}
			})
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "ncp-paused-creds")
			defer cleanupObjects(ctx, cluster, ncp)

			fakeAPI := fakeclient.NewClient()
			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeAPI.CreateCallCount).To(Equal(0))
		})

		It("should add the finalizer on first reconcile and short-circuit", func() {
			cluster := makeCluster(ctx, "fin-cluster")
			ncp := makeNCP(ctx, "fin-ncp", cluster, "fin-creds")
			makeCredSecret(ctx, "fin-creds")
			defer cleanupObjects(ctx, cluster, ncp)

			fakeAPI := fakeclient.NewClient()
			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &controlplanev1alpha1.NoPlaneControlPlane{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ncp.Name, Namespace: ns}, updated)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(updated, noplaneFinalizer)).To(BeTrue())
			Expect(fakeAPI.CreateCallCount).To(Equal(0))
		})

		It("should return error when the credentials secret does not exist", func() {
			cluster := makeCluster(ctx, "nosecret-cluster")
			ncp := makeNCP(ctx, "nosecret-ncp", cluster, "nonexistent-secret")
			addFinalizer(ctx, ncp)
			defer cleanupObjects(ctx, cluster, ncp)

			fakeAPI := fakeclient.NewClient()
			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("fetching API key"))
		})

		It("should return error when the credentials secret is missing the apiKey key", func() {
			cluster := makeCluster(ctx, "badkey-cluster")
			ncp := makeNCP(ctx, "badkey-ncp", cluster, "badkey-secret")
			addFinalizer(ctx, ncp)

			badSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "badkey-secret", Namespace: ns},
				Data:       map[string][]byte{"wrongKey": []byte("value")},
			}
			Expect(k8sClient.Create(ctx, badSecret)).To(Succeed())
			defer cleanupObjects(ctx, cluster, ncp, badSecret)

			fakeAPI := fakeclient.NewClient()
			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing 'apiKey' key"))
		})

		It("should return error when ClientFactory fails", func() {
			cluster := makeCluster(ctx, "factory-cluster")
			ncp := makeNCP(ctx, "factory-ncp", cluster, "factory-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "factory-creds")
			defer cleanupObjects(ctx, cluster, ncp)

			r := &NoPlaneControlPlaneReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				ClientFactory: func(apiKey string) (noplane.ClientInterface, error) {
					return nil, fmt.Errorf("auth failed")
				},
			}
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("creating API client"))
		})
	})

	Context("reconcileNormal happy path", func() {
		It("should create plane, set status, create secrets, and requeue 60s", func() {
			cluster := makeCluster(ctx, "happy-cluster")
			ncp := makeNCP(ctx, "happy-ncp", cluster, "happy-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "happy-creds")
			defer cleanupObjects(ctx, cluster, ncp)
			defer cleanupTestSecrets(ctx, cluster.Name)

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig
			r := makeReconciler(fakeAPI)

			result, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(60 * time.Second))
			Expect(fakeAPI.CreateCallCount).To(Equal(1))

			// Verify status
			updated := &controlplanev1alpha1.NoPlaneControlPlane{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ncp.Name, Namespace: ns}, updated)).To(Succeed())
			Expect(updated.Status.PlaneID).To(Equal("fake-np-id"))
			Expect(updated.Status.Ready).To(BeTrue())
			Expect(updated.Status.Initialized).To(BeTrue())
			Expect(updated.Status.Version).To(Equal("v1.29.2"))

			// Verify kubeconfig secret
			kubeconfigSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: cluster.Name + "-kubeconfig", Namespace: ns,
			}, kubeconfigSecret)).To(Succeed())
			Expect(kubeconfigSecret.Data["value"]).To(Equal(testKubeconfig))
			Expect(kubeconfigSecret.Labels[clusterv1.ClusterNameLabel]).To(Equal(cluster.Name))

			// Verify CA secret
			caSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: cluster.Name + "-ca", Namespace: ns,
			}, caSecret)).To(Succeed())
			Expect(caSecret.Data["tls.crt"]).To(Equal(testCACertPEM))
			Expect(caSecret.Data["tls.key"]).NotTo(BeEmpty())
			Expect(caSecret.Type).To(Equal(clusterv1.ClusterSecretType))

			// Verify endpoint was synced
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ncp.Name, Namespace: ns}, updated)).To(Succeed())
			Expect(updated.Spec.ControlPlaneEndpoint.Host).To(Equal("fake.noplane.io"))
			Expect(updated.Spec.ControlPlaneEndpoint.Port).To(Equal(int32(6443)))
		})
	})

	Context("reconcileNormal plane not ready", func() {
		It("should requeue after 15s when the plane is not ready", func() {
			cluster := makeCluster(ctx, "notready-cluster")
			ncp := makeNCP(ctx, "notready-ncp", cluster, "notready-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "notready-creds")
			defer cleanupObjects(ctx, cluster, ncp)

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig

			// Pre-populate a "created" (not ready) plane and set status.PlaneID
			// so resolvePlaneID takes the crash-recovery path.
			fakeAPI.Planes["existing-id"] = &noplane.Plane{
				ID:                "existing-id",
				Name:              ncp.Name,
				Status:            "created",
				KubernetesVersion: "v1.29.2",
				Endpoint:          noplane.Endpoint{Host: "fake.noplane.io", Port: 6443},
			}
			ncp.Status.PlaneID = "existing-id"
			Expect(k8sClient.Status().Update(ctx, ncp)).To(Succeed())

			r := makeReconciler(fakeAPI)
			result, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(15 * time.Second))
			Expect(fakeAPI.CreateCallCount).To(Equal(0))
		})
	})

	Context("reconcileNormal version mismatch", func() {
		It("should call UpdatePlane and requeue after 30s", func() {
			cluster := makeCluster(ctx, "version-cluster")
			ncp := makeNCP(ctx, "version-ncp", cluster, "version-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "version-creds")
			defer cleanupObjects(ctx, cluster, ncp)

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig

			// Plane is ready but has a different version.
			fakeAPI.Planes["ver-id"] = &noplane.Plane{
				ID:                "ver-id",
				Name:              ncp.Name,
				Status:            "ready",
				KubernetesVersion: "v1.28.0",
				Endpoint:          noplane.Endpoint{Host: "fake.noplane.io", Port: 6443},
			}
			fakeAPI.PlanesByName[ncp.Name] = fakeAPI.Planes["ver-id"]
			ncp.Status.PlaneID = "ver-id"
			Expect(k8sClient.Status().Update(ctx, ncp)).To(Succeed())

			r := makeReconciler(fakeAPI)
			result, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))
			Expect(fakeAPI.UpdateCallCount).To(Equal(1))
		})
	})

	Context("reconcileNormal 409 conflict on create", func() {
		It("should fall back to GetPlaneByName on 409 and adopt the existing plane", func() {
			cluster := makeCluster(ctx, "conflict-cluster")
			ncp := makeNCP(ctx, "conflict-ncp", cluster, "conflict-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "conflict-creds")
			defer cleanupObjects(ctx, cluster, ncp)
			defer cleanupTestSecrets(ctx, cluster.Name)

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig
			fakeAPI.CreateErr = noplane.NewConflictError(ncp.Name)

			// Pre-populate the plane that will be found by name.
			existingPlane := &noplane.Plane{
				ID:                "adopted-id",
				Name:              ncp.Name,
				Status:            "ready",
				KubernetesVersion: "v1.29.2",
				Endpoint:          noplane.Endpoint{Host: "fake.noplane.io", Port: 6443},
			}
			fakeAPI.Planes["adopted-id"] = existingPlane
			fakeAPI.PlanesByName[ncp.Name] = existingPlane

			r := makeReconciler(fakeAPI)
			result, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(60 * time.Second))
			Expect(fakeAPI.CreateCallCount).To(Equal(1))
			Expect(fakeAPI.GetByNameCallCount).To(Equal(1))

			updated := &controlplanev1alpha1.NoPlaneControlPlane{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ncp.Name, Namespace: ns}, updated)).To(Succeed())
			Expect(updated.Status.PlaneID).To(Equal("adopted-id"))
		})
	})

	Context("reconcileNormal GetPlane error", func() {
		It("should return error when GetPlane fails", func() {
			cluster := makeCluster(ctx, "geterr-cluster")
			ncp := makeNCP(ctx, "geterr-ncp", cluster, "geterr-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "geterr-creds")
			defer cleanupObjects(ctx, cluster, ncp)

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig
			// Pre-set status.PlaneID so resolvePlaneID skips create,
			// then GetErr triggers on the subsequent GetPlane call.
			ncp.Status.PlaneID = "some-id"
			Expect(k8sClient.Status().Update(ctx, ncp)).To(Succeed())
			fakeAPI.GetErr = fmt.Errorf("api down")

			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("getting plane"))
		})
	})

	Context("reconcileNormal brownfield adoption", func() {
		It("should use spec.PlaneID without calling CreatePlane", func() {
			cluster := makeCluster(ctx, "brown-cluster")
			ncp := makeNCP(ctx, "brown-ncp", cluster, "brown-creds", func(n *controlplanev1alpha1.NoPlaneControlPlane) {
				n.Spec.PlaneID = "pre-existing-id"
			})
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "brown-creds")
			defer cleanupObjects(ctx, cluster, ncp)
			defer cleanupTestSecrets(ctx, cluster.Name)

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig
			fakeAPI.Planes["pre-existing-id"] = &noplane.Plane{
				ID:                "pre-existing-id",
				Name:              ncp.Name,
				Status:            "ready",
				KubernetesVersion: "v1.29.2",
				Endpoint:          noplane.Endpoint{Host: "fake.noplane.io", Port: 6443},
			}

			r := makeReconciler(fakeAPI)
			result, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(60 * time.Second))
			Expect(fakeAPI.CreateCallCount).To(Equal(0))
			Expect(fakeAPI.GetCallCount).To(Equal(1))
		})
	})

	Context("reconcileNormal crash recovery", func() {
		It("should use status.PlaneID without calling CreatePlane", func() {
			cluster := makeCluster(ctx, "crash-cluster")
			ncp := makeNCP(ctx, "crash-ncp", cluster, "crash-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "crash-creds")
			defer cleanupObjects(ctx, cluster, ncp)
			defer cleanupTestSecrets(ctx, cluster.Name)

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig
			fakeAPI.Planes["recovered-id"] = &noplane.Plane{
				ID:                "recovered-id",
				Name:              ncp.Name,
				Status:            "ready",
				KubernetesVersion: "v1.29.2",
				Endpoint:          noplane.Endpoint{Host: "fake.noplane.io", Port: 6443},
			}

			ncp.Status.PlaneID = "recovered-id"
			Expect(k8sClient.Status().Update(ctx, ncp)).To(Succeed())

			r := makeReconciler(fakeAPI)
			result, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(60 * time.Second))
			Expect(fakeAPI.CreateCallCount).To(Equal(0))
			Expect(fakeAPI.GetCallCount).To(Equal(1))
		})
	})

	Context("reconcileNormal kubeconfig secret update", func() {
		It("should update an existing kubeconfig secret with new data", func() {
			cluster := makeCluster(ctx, "kcupd-cluster")
			ncp := makeNCP(ctx, "kcupd-ncp", cluster, "kcupd-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "kcupd-creds")
			defer cleanupObjects(ctx, cluster, ncp)
			defer cleanupTestSecrets(ctx, cluster.Name)

			// Pre-create the kubeconfig secret with old data
			oldKCSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cluster.Name + "-kubeconfig",
					Namespace: ns,
				},
				Data: map[string][]byte{"value": []byte("old-kubeconfig")},
			}
			Expect(k8sClient.Create(ctx, oldKCSecret)).To(Succeed())

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig

			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())

			updatedSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: cluster.Name + "-kubeconfig", Namespace: ns,
			}, updatedSecret)).To(Succeed())
			Expect(updatedSecret.Data["value"]).To(Equal(testKubeconfig))
		})
	})

	Context("reconcileNormal CA secret skip", func() {
		It("should not regenerate CA secret if tls.key is already present", func() {
			cluster := makeCluster(ctx, "caskip-cluster")
			ncp := makeNCP(ctx, "caskip-ncp", cluster, "caskip-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "caskip-creds")
			defer cleanupObjects(ctx, cluster, ncp)
			defer cleanupTestSecrets(ctx, cluster.Name)

			// Pre-create the CA secret with existing tls.key
			existingCASecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cluster.Name + "-ca",
					Namespace: ns,
				},
				Data: map[string][]byte{
					"tls.crt": []byte("existing-cert"),
					"tls.key": []byte("existing-key"),
				},
			}
			Expect(k8sClient.Create(ctx, existingCASecret)).To(Succeed())

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig

			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify the CA secret was NOT regenerated
			caSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: cluster.Name + "-ca", Namespace: ns,
			}, caSecret)).To(Succeed())
			Expect(caSecret.Data["tls.key"]).To(Equal([]byte("existing-key")))
		})
	})

	Context("reconcileNormal GetKubeconfig error", func() {
		It("should return error when GetKubeconfig fails", func() {
			cluster := makeCluster(ctx, "kcerr-cluster")
			ncp := makeNCP(ctx, "kcerr-ncp", cluster, "kcerr-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "kcerr-creds")
			defer cleanupObjects(ctx, cluster, ncp)

			fakeAPI := fakeclient.NewClient()
			fakeAPI.KubeconfigErr = fmt.Errorf("upstream connection failed")

			// Pre-populate a ready plane so we reach reconcileKubeconfig.
			fakeAPI.Planes["kc-id"] = &noplane.Plane{
				ID:                "kc-id",
				Name:              ncp.Name,
				Status:            "ready",
				KubernetesVersion: "v1.29.2",
				Endpoint:          noplane.Endpoint{Host: "fake.noplane.io", Port: 6443},
			}
			ncp.Status.PlaneID = "kc-id"
			Expect(k8sClient.Status().Update(ctx, ncp)).To(Succeed())

			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("fetching kubeconfig"))
		})
	})

	Context("reconcileDelete", func() {
		It("should remove finalizer without calling DeletePlane when no planeID", func() {
			cluster := makeCluster(ctx, "delnoid-cluster")
			ncp := makeNCP(ctx, "delnoid-ncp", cluster, "delnoid-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "delnoid-creds")
			defer cleanupObjects(ctx, cluster)

			// Trigger deletion
			Expect(k8sClient.Delete(ctx, ncp)).To(Succeed())

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig
			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeAPI.DeleteCallCount).To(Equal(0))

			// Object should be gone
			err = k8sClient.Get(ctx, types.NamespacedName{Name: ncp.Name, Namespace: ns},
				&controlplanev1alpha1.NoPlaneControlPlane{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("should call DeletePlane and remove finalizer when planeID is set", func() {
			cluster := makeCluster(ctx, "delok-cluster")
			ncp := makeNCP(ctx, "delok-ncp", cluster, "delok-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "delok-creds")
			defer cleanupObjects(ctx, cluster)

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig
			fakeAPI.Planes["del-id"] = &noplane.Plane{
				ID: "del-id", Name: ncp.Name, Status: "ready",
				KubernetesVersion: "v1.29.2",
				Endpoint:          noplane.Endpoint{Host: "fake.noplane.io", Port: 6443},
			}
			fakeAPI.PlanesByName[ncp.Name] = fakeAPI.Planes["del-id"]

			// Set status.PlaneID
			ncp.Status.PlaneID = "del-id"
			Expect(k8sClient.Status().Update(ctx, ncp)).To(Succeed())

			// Trigger deletion
			Expect(k8sClient.Delete(ctx, ncp)).To(Succeed())

			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeAPI.DeleteCallCount).To(Equal(1))

			// Object should be gone
			err = k8sClient.Get(ctx, types.NamespacedName{Name: ncp.Name, Namespace: ns},
				&controlplanev1alpha1.NoPlaneControlPlane{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("should tolerate 404 from DeletePlane and remove finalizer", func() {
			cluster := makeCluster(ctx, "del404-cluster")
			ncp := makeNCP(ctx, "del404-ncp", cluster, "del404-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "del404-creds")
			defer cleanupObjects(ctx, cluster)

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig
			fakeAPI.DeleteErr = noplane.NewNotFoundError("gone-id")

			ncp.Status.PlaneID = "gone-id"
			Expect(k8sClient.Status().Update(ctx, ncp)).To(Succeed())

			// Trigger deletion
			Expect(k8sClient.Delete(ctx, ncp)).To(Succeed())

			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeAPI.DeleteCallCount).To(Equal(1))

			// Object should be gone
			err = k8sClient.Get(ctx, types.NamespacedName{Name: ncp.Name, Namespace: ns},
				&controlplanev1alpha1.NoPlaneControlPlane{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("should return error and keep finalizer when DeletePlane fails with non-404", func() {
			cluster := makeCluster(ctx, "delerr-cluster")
			ncp := makeNCP(ctx, "delerr-ncp", cluster, "delerr-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "delerr-creds")
			defer cleanupObjects(ctx, cluster, ncp)

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig
			fakeAPI.DeleteErr = fmt.Errorf("internal server error")

			ncp.Status.PlaneID = "fail-id"
			Expect(k8sClient.Status().Update(ctx, ncp)).To(Succeed())

			// Trigger deletion
			Expect(k8sClient.Delete(ctx, ncp)).To(Succeed())

			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("deleting plane"))

			// Object should still exist with finalizer (deletion blocked)
			remaining := &controlplanev1alpha1.NoPlaneControlPlane{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ncp.Name, Namespace: ns}, remaining)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(remaining, noplaneFinalizer)).To(BeTrue())
		})
	})

	Context("reconcileNormal CreatePlane non-conflict error", func() {
		It("should return error when CreatePlane fails with a non-409 error", func() {
			cluster := makeCluster(ctx, "createerr-cluster")
			ncp := makeNCP(ctx, "createerr-ncp", cluster, "createerr-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "createerr-creds")
			defer cleanupObjects(ctx, cluster, ncp)

			fakeAPI := fakeclient.NewClient()
			fakeAPI.CreateErr = fmt.Errorf("internal server error")

			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("creating plane"))
		})
	})

	Context("reconcileNormal UpdatePlane error", func() {
		It("should return error when UpdatePlane fails", func() {
			cluster := makeCluster(ctx, "upderr-cluster")
			ncp := makeNCP(ctx, "upderr-ncp", cluster, "upderr-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "upderr-creds")
			defer cleanupObjects(ctx, cluster, ncp)

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig
			fakeAPI.UpdateErr = fmt.Errorf("upgrade not supported")

			fakeAPI.Planes["upd-id"] = &noplane.Plane{
				ID:                "upd-id",
				Name:              ncp.Name,
				Status:            "ready",
				KubernetesVersion: "v1.28.0", // mismatch with spec v1.29.2
				Endpoint:          noplane.Endpoint{Host: "fake.noplane.io", Port: 6443},
			}
			ncp.Status.PlaneID = "upd-id"
			Expect(k8sClient.Status().Update(ctx, ncp)).To(Succeed())

			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("upgrading plane"))
		})
	})

	Context("reconcileNormal CA secret edge cases", func() {
		It("should update existing CA secret that has no tls.key", func() {
			cluster := makeCluster(ctx, "caupd-cluster")
			ncp := makeNCP(ctx, "caupd-ncp", cluster, "caupd-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "caupd-creds")
			defer cleanupObjects(ctx, cluster, ncp)
			defer cleanupTestSecrets(ctx, cluster.Name)

			// Pre-create a CA secret WITHOUT tls.key (should be updated, not skipped)
			existingCASecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cluster.Name + "-ca",
					Namespace: ns,
				},
				Type: clusterv1.ClusterSecretType,
				Data: map[string][]byte{
					"tls.crt": []byte("old-cert"),
				},
			}
			Expect(k8sClient.Create(ctx, existingCASecret)).To(Succeed())

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig

			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify the CA secret was updated with real CA and a generated tls.key
			caSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: cluster.Name + "-ca", Namespace: ns,
			}, caSecret)).To(Succeed())
			Expect(caSecret.Data["tls.crt"]).To(Equal(testCACertPEM))
			Expect(caSecret.Data["tls.key"]).NotTo(BeEmpty())
		})

		It("should recreate CA secret when existing has wrong type", func() {
			cluster := makeCluster(ctx, "catype-cluster")
			ncp := makeNCP(ctx, "catype-ncp", cluster, "catype-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "catype-creds")
			defer cleanupObjects(ctx, cluster, ncp)
			defer cleanupTestSecrets(ctx, cluster.Name)

			// Pre-create a CA secret with wrong type (Opaque instead of ClusterSecretType)
			wrongTypeSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cluster.Name + "-ca",
					Namespace: ns,
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"tls.crt": []byte("old-cert"),
				},
			}
			Expect(k8sClient.Create(ctx, wrongTypeSecret)).To(Succeed())

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = testKubeconfig

			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify the CA secret was recreated with the correct type
			caSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: cluster.Name + "-ca", Namespace: ns,
			}, caSecret)).To(Succeed())
			Expect(caSecret.Type).To(Equal(clusterv1.ClusterSecretType))
			Expect(caSecret.Data["tls.crt"]).To(Equal(testCACertPEM))
			Expect(caSecret.Data["tls.key"]).NotTo(BeEmpty())
		})

		It("should return error when kubeconfig has no clusters", func() {
			cluster := makeCluster(ctx, "noclusters-cluster")
			ncp := makeNCP(ctx, "noclusters-ncp", cluster, "noclusters-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "noclusters-creds")
			defer cleanupObjects(ctx, cluster, ncp)
			defer cleanupTestSecrets(ctx, cluster.Name)

			// Build a kubeconfig with no cluster entries
			emptyKC := clientcmdapi.NewConfig()
			emptyKC.AuthInfos["admin"] = &clientcmdapi.AuthInfo{}
			emptyKC.Contexts["test"] = &clientcmdapi.Context{AuthInfo: "admin"}
			emptyKC.CurrentContext = "test"
			emptyKCBytes, kcErr := clientcmd.Write(*emptyKC)
			Expect(kcErr).NotTo(HaveOccurred())

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = emptyKCBytes

			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("kubeconfig contains no clusters"))
		})

		It("should return error when kubeconfig cluster has no CA data", func() {
			cluster := makeCluster(ctx, "noca-cluster")
			ncp := makeNCP(ctx, "noca-ncp", cluster, "noca-creds")
			addFinalizer(ctx, ncp)
			makeCredSecret(ctx, "noca-creds")
			defer cleanupObjects(ctx, cluster, ncp)
			defer cleanupTestSecrets(ctx, cluster.Name)

			// Build a kubeconfig with a cluster but no CA data
			noCAKC := clientcmdapi.NewConfig()
			noCAKC.Clusters["test"] = &clientcmdapi.Cluster{
				Server: "https://fake.noplane.io:6443",
			}
			noCAKC.AuthInfos["admin"] = &clientcmdapi.AuthInfo{}
			noCAKC.Contexts["test"] = &clientcmdapi.Context{Cluster: "test", AuthInfo: "admin"}
			noCAKC.CurrentContext = "test"
			noCAKCBytes, kcErr := clientcmd.Write(*noCAKC)
			Expect(kcErr).NotTo(HaveOccurred())

			fakeAPI := fakeclient.NewClient()
			fakeAPI.Kubeconfig = noCAKCBytes

			r := makeReconciler(fakeAPI)
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ncp.Name, Namespace: ns},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no certificate-authority-data"))
		})
	})
})
