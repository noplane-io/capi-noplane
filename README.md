# capi-noplane

A [Cluster API](https://cluster-api.sigs.k8s.io/) control plane provider that delegates Kubernetes control plane lifecycle to the [noplane.io](https://noplane.io) hosted service. Worker nodes bootstrap using standard [Kubeadm (CABPK)](https://cluster-api.sigs.k8s.io/tasks/bootstrap/kubeadm-bootstrap/) — no custom bootstrap provider is needed.

## Quick Start

This guide creates a Kubernetes cluster with a NoPlane-managed control plane and Hetzner Cloud worker nodes.

### Prerequisites

- A Kubernetes management cluster
- [clusterctl](https://cluster-api.sigs.k8s.io/user/quick-start#install-clusterctl) installed
- A [noplane.io](https://noplane.io) account and API key
- A [Hetzner Cloud](https://www.hetzner.com/cloud) account and API token

### 1. Configure clusterctl

Add the NoPlane provider to `~/.cluster-api/clusterctl.yaml`:

```yaml
providers:
  - name: "noplane"
    url: "https://github.com/noplane-io/capi-noplane/releases/latest/control-plane-components.yaml"
    type: "ControlPlaneProvider"
```

### 2. Initialize providers

```sh
clusterctl init \
  --infrastructure hetzner \
  --bootstrap kubeadm \
  --control-plane noplane
```

### 3. Create credentials

```sh
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: noplane-credentials
  namespace: default
  labels:
    clusterctl.cluster.x-k8s.io/move: ""
type: Opaque
stringData:
  apiKey: "<YOUR_NOPLANE_API_KEY>"
---
apiVersion: v1
kind: Secret
metadata:
  name: hetzner
  namespace: default
  labels:
    clusterctl.cluster.x-k8s.io/move: ""
type: Opaque
stringData:
  hcloud: "<YOUR_HCLOUD_TOKEN>"
EOF
```

### 4. Create a cluster

```sh
kubectl apply -f docs/examples/my-cluster.yaml
```

This creates:
- A NoPlane-managed control plane running Kubernetes v1.33.0
- One Hetzner Cloud worker node (Ubuntu 24.04, cpx22) with kubeadm bootstrap

See [`docs/examples/my-cluster.yaml`](docs/examples/my-cluster.yaml) for the full manifest. Edit the SSH key name and cluster name before applying.

### 5. Access the cluster

```sh
clusterctl get kubeconfig my-cluster > my-cluster.kubeconfig
kubectl --kubeconfig my-cluster.kubeconfig get nodes
```

## Development

```sh
make build          # generate manifests, deepcopy, format, vet, compile
make test           # unit tests with envtest
make run            # run controller locally against current kubeconfig
make lint           # golangci-lint
make manifests      # regenerate CRDs and RBAC
make release IMG=<registry>/capi-noplane:<tag>  # build clusterctl release artifacts
```

## License

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
