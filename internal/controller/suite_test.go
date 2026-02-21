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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	controlplanev1alpha1 "github.com/noplane-io/capi-noplane/api/v1alpha1"
	"github.com/noplane-io/capi-noplane/internal/api/noplane"
	fakeclient "github.com/noplane-io/capi-noplane/internal/api/noplane/fake"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client

	testCACertPEM  []byte
	testKubeconfig []byte
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	var err error
	err = controlplanev1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = clusterv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	By("generating test CA certificate and kubeconfig")
	generateTestKubeconfig()

	By("bootstrapping test environment")
	crdPaths := []string{filepath.Join("..", "..", "config", "crd", "bases")}
	if p := capiCRDPath(); p != "" {
		crdPaths = append(crdPaths, p)
	}
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     crdPaths,
		ErrorIfCRDPathMissing: true,
	}

	// Retrieve the first found binary directory to allow running tests from IDEs
	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
	}

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	Eventually(func() error {
		return testEnv.Stop()
	}, time.Minute, time.Second).Should(Succeed())
})

// generateTestKubeconfig creates a self-signed CA cert and a valid kubeconfig
// containing it, for use in tests that exercise reconcileCASecret.
func generateTestKubeconfig() {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())

	testCACertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	kubeconfigObj := clientcmdapi.NewConfig()
	kubeconfigObj.Clusters["test"] = &clientcmdapi.Cluster{
		Server:                   "https://fake.noplane.io:6443",
		CertificateAuthorityData: testCACertPEM,
	}
	kubeconfigObj.AuthInfos["admin"] = &clientcmdapi.AuthInfo{}
	kubeconfigObj.Contexts["test"] = &clientcmdapi.Context{Cluster: "test", AuthInfo: "admin"}
	kubeconfigObj.CurrentContext = "test"

	testKubeconfig, err = clientcmd.Write(*kubeconfigObj)
	Expect(err).NotTo(HaveOccurred())
}

// capiCRDPath resolves the CAPI module directory from the Go module cache
// and returns the path to its CRD bases directory.
func capiCRDPath() string {
	out, err := exec.Command("go", "list", "-m", "-json", "sigs.k8s.io/cluster-api").Output()
	if err != nil {
		return ""
	}
	var mod struct{ Dir string }
	if json.Unmarshal(out, &mod) != nil {
		return ""
	}
	return filepath.Join(mod.Dir, "config", "crd", "bases")
}

// --- Shared test helpers ---

func makeCluster(ctx context.Context, name, namespace string) *clusterv1.Cluster {
	// Use Unstructured to create the Cluster because the CAPI v1beta2 CRD
	// requires "spec" in the JSON body, but the Go struct's omitzero tags
	// omit it when all fields are zero-valued.
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(clusterv1.GroupVersion.WithKind("Cluster"))
	u.SetName(name)
	u.SetNamespace(namespace)
	u.Object["spec"] = map[string]interface{}{
		"paused": false,
	}
	Expect(k8sClient.Create(ctx, u)).To(Succeed())

	// Read back as typed object so callers get a proper *Cluster with UID set.
	cluster := &clusterv1.Cluster{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, cluster)).To(Succeed())
	return cluster
}

func makeNCP(
	ctx context.Context,
	name, namespace string,
	cluster *clusterv1.Cluster,
	credSecretName string,
	opts ...func(*controlplanev1alpha1.NoPlaneControlPlane),
) *controlplanev1alpha1.NoPlaneControlPlane {
	ncp := &controlplanev1alpha1.NoPlaneControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Cluster",
					Name:       cluster.Name,
					UID:        cluster.UID,
				},
			},
		},
		Spec: controlplanev1alpha1.NoPlaneControlPlaneSpec{
			Version: "v1.29.2",
			CredentialsSecretRef: corev1.SecretReference{
				Name:      credSecretName,
				Namespace: namespace,
			},
		},
	}
	for _, opt := range opts {
		opt(ncp)
	}
	Expect(k8sClient.Create(ctx, ncp)).To(Succeed())
	return ncp
}

func makeCredSecret(ctx context.Context, name, namespace, apiKey string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"apiKey": []byte(apiKey),
		},
	}
	Expect(k8sClient.Create(ctx, secret)).To(Succeed())
	return secret
}

func makeReconciler(fakeAPI *fakeclient.Client) *NoPlaneControlPlaneReconciler {
	return &NoPlaneControlPlaneReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
		ClientFactory: func(apiKey string) (noplane.ClientInterface, error) {
			return fakeAPI, nil
		},
	}
}

func addFinalizer(ctx context.Context, ncp *controlplanev1alpha1.NoPlaneControlPlane) {
	controllerutil.AddFinalizer(ncp, noplaneFinalizer)
	Expect(k8sClient.Update(ctx, ncp)).To(Succeed())
}

func cleanupObjects(ctx context.Context, objs ...client.Object) {
	for _, obj := range objs {
		_ = client.IgnoreNotFound(k8sClient.Delete(ctx, obj))
	}
}

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
// ENVTEST-based tests depend on specific binaries, usually located in paths set by
// controller-runtime. When running tests directly (e.g., via an IDE) without using
// Makefile targets, the 'BinaryAssetsDirectory' must be explicitly configured.
//
// This function streamlines the process by finding the required binaries, similar to
// setting the 'KUBEBUILDER_ASSETS' environment variable. To ensure the binaries are
// properly set up, run 'make setup-envtest' beforehand.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
