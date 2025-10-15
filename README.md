# ConfigMirror Operator

A Kubernetes operator that replicates ConfigMaps across namespaces with PostgreSQL persistence.

## Features

- Replicates ConfigMaps from source namespace to target namespaces based on label selectors
- Removes replicated ConfigMaps when source is deleted
- Stores ConfigMap data in RDS PostgreSQL
- Supports leader election for multi-replica deployments
- Non-root containers, read-only filesystem, dropped capabilities

## Architecture

```
ConfigMirror CR (ops namespace)
├── Watches ConfigMaps in sourceNamespace
├── Filters by label selector
├── Replicates to target namespaces
│   ├── namespace-a
│   ├── namespace-b
│   └── namespace-c
└── Saves to PostgreSQL RDS
```

## Prerequisites

### Infrastructure Requirements

This operator requires AWS infrastructure to be deployed first:
- EKS cluster (Kubernetes 1.34)
- RDS PostgreSQL 17.6 database
- ECR repository for operator images
- VPC with Multi-AZ setup
- IAM roles and OIDC provider

**Deploy infrastructure first using:** https://github.com/sarataha/pawapay-infra

### Local Development Requirements

- kubectl 1.34+
- Helm 3.16+
- AWS CLI 2.0+
- Go 1.25.0+ (for development)
- Kubebuilder 4.9.0+ (for development)

## Installation

### 1. Configure kubectl

```bash
# Connect to the EKS cluster created by pawapay-infra
aws eks update-kubeconfig --name pawapay-eks-dev --region us-east-1
kubectl get nodes
```

### 2. Create Kubernetes Secret from AWS Secrets Manager

RDS credentials are stored in AWS Secrets Manager. Create a Kubernetes secret from it:

```bash
# Create namespace
kubectl create namespace configmirror-system

# Sync credentials from AWS Secrets Manager to Kubernetes
aws secretsmanager get-secret-value \
  --secret-id pawapay-rds-master-password \
  --region us-east-1 \
  --query SecretString \
  --output text | jq -r '. | to_entries | map("--from-literal=\(.key)=\(.value|tostring)") | join(" ")' | \
  xargs kubectl create secret generic rds-credentials -n configmirror-system

# Verify
kubectl describe secret rds-credentials -n configmirror-system
```

> **Note:** Due to time constraints this uses a manual sync command. In a production environment I would use [External Secrets Operator](https://external-secrets.io/) to automatically sync secrets from AWS Secrets Manager to Kubernetes eliminating manual steps and keeping secrets updated automatically.

### 3. Install with Helm

```bash
# Get ECR repository URL
export ECR_URL=$(aws ecr describe-repositories \
  --repository-names configmirror-operator \
  --query 'repositories[0].repositoryUri' \
  --output text)

# Install operator
helm install configmirror-operator ./helm/configmirror-operator \
  --namespace configmirror-system \
  --set image.repository=$ECR_URL \
  --set image.tag=latest

# Verify deployment
kubectl get pods -n configmirror-system
kubectl logs -n configmirror-system -l app.kubernetes.io/name=configmirror-operator
```

## Usage

### Create a ConfigMirror Resource

```yaml
apiVersion: mirror.pawapay.io/v1alpha1
kind: ConfigMirror
metadata:
  name: app-config-mirror
  namespace: ops
spec:
  sourceNamespace: app-source
  targetNamespaces:
    - dev
    - staging
    - prod
  selector:
    matchLabels:
      app: myapp
      replicate: "true"
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
  namespace: app-source
  labels:
    app: myapp
    replicate: "true"
data:
  config.yaml: |
    key: value
```

The operator will automatically:
1. Detect the ConfigMap in `app-source` namespace matches the selector
2. Replicate it to dev, staging, and prod namespaces
3. Save it to PostgreSQL database
4. Update the ConfigMirror status

### ConfigMap Update Behavior

The operator automatically handles updates to source ConfigMaps:

- When a source ConfigMap is modified, it auto-updates all replicas in target namespaces
- Changes to `data` and `binaryData` fields are immediately propagated
- Replicated ConfigMaps have ownership labels to prevent conflicts
- When a source ConfigMap is deleted, all replicated copies are automatically removed

### Finalizer Behavior

The operator uses finalizers for clean resource cleanup:

- A finalizer (`mirror.pawapay.io/finalizer`) is automatically added when ConfigMirror is created
- When ConfigMirror is deleted, the operator:
  1. Removes all replicated ConfigMaps from target namespaces
  2. Deletes database records (if enabled)
  3. Removes the finalizer to complete deletion
- This prevents orphaned ConfigMaps when ConfigMirror is deleted

### Check Status

```bash
kubectl get configmirrors -n ops
kubectl describe configmirror app-config-mirror -n ops
```

## Testing

See [docs/TESTING.md](docs/TESTING.md) for testing guide.

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

## CI/CD

The operator uses GitHub Actions for CI/CD:

- On PR: Runs tests, linting, Helm validation, and builds
- On main push: Builds Docker image and pushes to ECR using OIDC
- On tag: Creates versioned releases
- E2E Tests: Runs in Kind cluster on every push

Required GitHub secrets:
- `AWS_ROLE_ARN`: Full ARN of the IAM role (e.g., `arn:aws:iam::ACCOUNT_ID:role/github-actions-ecr-push`)

Required AWS resources:
- IAM OIDC provider for GitHub Actions (`token.actions.githubusercontent.com`)
- IAM role `github-actions-ecr-push` with trust policy and ECR push permissions

## Monitoring

### Metrics

The operator exposes Prometheus metrics on port 8080:

- `controller_runtime_reconcile_total`: Total reconciliations
- `controller_runtime_reconcile_errors_total`: Failed reconciliations
- `controller_runtime_reconcile_time_seconds`: Reconciliation duration

#### Accessing Metrics

```bash
# Port-forward to access metrics locally
kubectl port-forward -n configmirror-system svc/configmirror-operator-metrics 8080:8080
curl http://localhost:8080/metrics
```

### Health Checks

- Liveness: `http://:8081/healthz`
- Readiness: `http://:8081/readyz`

## Security

- Non-root container with distroless base image
- Read-only root filesystem
- Dropped all capabilities
- Database credentials stored in Kubernetes secrets
- NetworkPolicies supported
- Pod Security Standards compliant

## Design Decisions

- AWS infra is deployed via `pawapay-infra` before installing the operator
- Manual sync of RDS credentials from AWS Secrets Manager to Kubernetes (production would use External Secrets Operator)
- RDS uses password-based authentication (production would use IAM database authentication with IRSA)
- Operator handles missing database credentials gracefully and continues working without persistence
- Building only for linux/amd64 to keep CI faster
- Operator requires broader RBAC permissions to watch ConfigMaps across multiple namespaces

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
