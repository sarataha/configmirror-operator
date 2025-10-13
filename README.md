# ConfigMirror Operator

A Kubernetes operator that replicates ConfigMaps across namespaces with PostgreSQL persistence.

## Features

- **ConfigMap Replication**: Automatically replicates ConfigMaps from a source namespace to multiple target namespaces based on label selectors
- **Orphan Cleanup**: Automatically removes replicated ConfigMaps when source is deleted
- **PostgreSQL Persistence**: Stores ConfigMap data in RDS PostgreSQL for audit trails and recovery
- **IRSA Integration**: Uses IAM Roles for Service Accounts for secure AWS access
- **High Availability**: Supports leader election for multi-replica deployments
- **Security**: Non-root containers, read-only filesystem, dropped capabilities

## Architecture

```
ConfigMirror CR (source-ns)
├── Watches ConfigMaps with matching labels
├── Replicates to target namespaces
│   ├── namespace-a
│   ├── namespace-b
│   └── namespace-c
└── Saves to PostgreSQL RDS
```

## Prerequisites

- Kubernetes 1.34+ (EKS)
- PostgreSQL 17+ (RDS)
- Helm 3.16+
- AWS IAM role with RDS access (for IRSA)
- Go 1.25.0+ (for development)
- Kubebuilder 4.9.0+ (for development)

## Installation

### 1. Create Database Secret

```bash
kubectl create secret generic rds-credentials \
  --from-literal=host=your-rds-endpoint.rds.amazonaws.com \
  --from-literal=port=5432 \
  --from-literal=database=configmirror \
  --from-literal=username=your-username \
  --from-literal=password=your-password \
  -n configmirror-system
```

### 2. Install with Helm

```bash
helm install configmirror-operator ./helm/configmirror-operator \
  --namespace configmirror-system \
  --create-namespace \
  --set image.repository=<your-ecr-repo> \
  --set image.tag=latest \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::ACCOUNT_ID:role/configmirror-operator
```

## Usage

### Create a ConfigMirror Resource

```yaml
apiVersion: mirror.pawapay.io/v1alpha1
kind: ConfigMirror
metadata:
  name: app-config-mirror
  namespace: source-namespace
spec:
  selector:
    matchLabels:
      app: myapp
      replicate: "true"
  targetNamespaces:
    - dev
    - staging
    - prod
  database:
    enabled: true
    secretRef:
      name: rds-credentials
      namespace: configmirror-system
```

### Create ConfigMaps to be Replicated

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: source-namespace
  labels:
    app: myapp
    replicate: "true"
data:
  config.yaml: |
    key: value
```

The operator will automatically:
1. Detect the ConfigMap matches the selector
2. Replicate it to dev, staging, and prod namespaces
3. Save it to PostgreSQL database
4. Update the ConfigMirror status

### Check Status

```bash
kubectl get configmirrors -n source-namespace
kubectl describe configmirror app-config-mirror -n source-namespace
```

## Database Schema

The operator creates the following table:

```sql
CREATE TABLE configmaps (
    id SERIAL PRIMARY KEY,
    name VARCHAR(253) NOT NULL,
    namespace VARCHAR(253) NOT NULL,
    data JSONB NOT NULL,
    labels JSONB,
    annotations JSONB,
    configmirror_name VARCHAR(253) NOT NULL,
    configmirror_namespace VARCHAR(253) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE(name, namespace, configmirror_namespace, configmirror_name)
);
```

## Development

### Build and Run Locally

```bash
# Install CRDs
make install

# Run locally (connects to kubeconfig cluster)
make run

# Run tests
make test

# Build binary
make build
```

### Build Docker Image

```bash
make docker-build IMG=<registry>/<image>:<tag>
make docker-push IMG=<registry>/<image>:<tag>
```

## CI/CD

The operator uses GitHub Actions for CI/CD:

- **On PR**: Runs tests, linting, Helm validation, and builds
- **On main push**: Builds multi-platform Docker images and pushes to ECR using OIDC
- **On tag**: Creates versioned releases
- **E2E Tests**: Runs in Kind cluster on every push

Required GitHub secrets:
- `AWS_ROLE_ARN`: Full ARN of the IAM role (e.g., `arn:aws:iam::ACCOUNT_ID:role/github-actions-ecr-push`)

Required AWS resources:
- IAM OIDC provider for GitHub Actions (`token.actions.githubusercontent.com`)
- IAM role `github-actions-ecr-push` with:
  - Trust policy allowing GitHub OIDC
  - ECR push permissions to the repository

## Monitoring

### Metrics

The operator exposes Prometheus metrics on port 8080:

- `controller_runtime_reconcile_total`: Total reconciliations
- `controller_runtime_reconcile_errors_total`: Failed reconciliations
- `controller_runtime_reconcile_time_seconds`: Reconciliation duration

### Health Checks

- Liveness: `http://:8081/healthz`
- Readiness: `http://:8081/readyz`

## Security

- Non-root container with distroless base image
- Read-only root filesystem
- Dropped all capabilities
- IRSA for AWS credentials (no hardcoded secrets)
- NetworkPolicies supported
- Pod Security Standards compliant

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

