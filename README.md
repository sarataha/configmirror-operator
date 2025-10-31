# ConfigMirror Operator

A Kubernetes operator that replicates ConfigMaps across namespaces with PostgreSQL persistence.

## Features

- Replicates ConfigMaps across namespaces using label selectors
- Auto-cleanup when source ConfigMap is deleted
- Persist ConfigMap data to PostgreSQL
- Leader election for HA deployments
- Runs as non-root with minimal privileges

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

### Infra Requirements

This operator requires AWS infrastructure to be deployed first:
- EKS cluster (Kubernetes 1.34)
- RDS PostgreSQL 17.6 database
- ECR repository for operator images
- VPC with Multi-AZ setup
- IAM roles and OIDC provider

**Deploy infrastructure first using:** https://github.com/sarataha/infra

### Requirements

- kubectl 1.34+
- Helm 3.16+
- AWS CLI 2.0+
- Go 1.25.0+
- Kubebuilder 4.9.0+

## Installation

### 1. Build and Push Image

```bash
# Get ECR repository URL
export ECR_URL=$(aws ecr describe-repositories --repository-names configmirror-operator --query 'repositories[0].repositoryUri' --output text)

# Login to ECR
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin $ECR_URL

# Build and push image
export IMAGE_TAG="ts-$(date -u +%Y%m%d%H%M%S)"
docker build -t $ECR_URL:$IMAGE_TAG .
docker push $ECR_URL:$IMAGE_TAG
```

**Note**: ECR is immutable not allowing overwriting tags so for testing we could use a timestamp for each build. In prod typically the images used is the one pushed by CI/CD with proper version tags.

### 2. Configure kubectl

```bash
# Connect to the EKS cluster created by infra repo
aws eks update-kubeconfig --name configmirror-eks-dev --region us-east-1
kubectl get nodes
```

### 3. Set Up Secret Synchronization

RDS credentials are stored in AWS Secrets Manager and automatically synced to Kubernetes using External Secrets Operator (deployed via the infra repo).

```bash
# Create namespace
kubectl create namespace configmirror-system

# Apply ClusterSecretStore (connects to AWS Secrets Manager)
kubectl apply -f config/samples/cluster-secret-store.yaml

# Apply ExternalSecret (syncs RDS credentials to K8s secret)
kubectl apply -f config/samples/external-secret-rds.yaml

# Verify secret was created
kubectl get secret rds-credentials -n configmirror-system
kubectl describe externalsecret rds-credentials -n configmirror-system
```

The ExternalSecret will automatically keep the Kubernetes secret in sync with AWS Secrets Manager - no manual updates needed.

### 4. Install with Helm

```bash
# Get ECR repository URL
export ECR_URL=$(aws ecr describe-repositories --repository-names configmirror-operator --query 'repositories[0].repositoryUri' --output text)

# Install operator
helm install configmirror-operator ./helm/configmirror-operator \
  --namespace configmirror-system \
  --set image.repository=$ECR_URL \
  --set image.tag=$IMAGE_TAG

# Verify deployment
kubectl get pods -n configmirror-system
kubectl logs -n configmirror-system -l app.kubernetes.io/name=configmirror-operator
```

## Usage

### Create a ConfigMirror Resource

```yaml
apiVersion: mirror.configmirror.io/v1alpha1
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

- A finalizer (`mirror.configmirror.io/finalizer`) is automatically added when ConfigMirror is created
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

- `rest_client_requests_total`: Kubernetes API client requests by status code, method, and host
- `leader_election_master_status`: Leader election status (1 = leader, 0 = follower)
- Process metrics: CPU, memory, file descriptors, etc.
- Go runtime metrics: GC stats, goroutines, memory allocations

#### Accessing Metrics

```bash
# Port-forward to access metrics
kubectl port-forward -n configmirror-system deployment/configmirror-operator 8080:8080
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

## Design Decisions & Assumptions

### Assumptions Made
- AWS infrastructure (EKS, RDS, ECR) and External Secrets Operator are deployed via `infra` repo before installing the operator
- All resources deployed in us-east-1 region
- Operator works with or without database connection
- ConfigMaps selected using standard Kubernetes label selectors
- Operator requires cluster-wide permissions to watch multiple namespaces

### Design Decisions
- Using External Secrets Operator to automatically sync RDS credentials from AWS Secrets Manager - keeps secrets updated without manual intervention
- Using password-based auth for the database to keep the setup straightforward. IAM auth would be better for prod
- Building only for linux/amd64 to keep CI builds fast. ARM64 support can come later if needed
- The operator keeps running even without database access, which made testing way easier

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
