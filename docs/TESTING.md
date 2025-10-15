# Testing Guide

This guide covers manual testing scenarios for the ConfigMirror operator.

## Prerequisites

- Operator deployed and running in the cluster
- kubectl configured and connected to the cluster

## Basic Replication Test

### 1. Create Test Namespaces

```bash
kubectl create namespace app-source
kubectl create namespace app-staging
kubectl create namespace app-prod
```

### 2. Create ConfigMirror Resource

Create a file `test-configmirror.yaml`:

```yaml
apiVersion: mirror.pawapay.io/v1alpha1
kind: ConfigMirror
metadata:
  name: test-mirror
  namespace: configmirror-system
spec:
  sourceNamespace: app-source
  targetNamespaces:
    - app-staging
    - app-prod
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

Apply it:

```bash
kubectl apply -f test-configmirror.yaml
```

### 3. Create Test ConfigMap

Create a file `test-configmap.yaml`:

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
    app:
      name: myapp
      version: 1.0.0
  env: production
```

Apply it:

```bash
kubectl apply -f test-configmap.yaml
```

### 4. Verify Replication

```bash
# Check replicas exist
kubectl get configmap app-config -n app-staging
kubectl get configmap app-config -n app-prod

# Verify data is identical
kubectl get configmap app-config -n app-staging -o yaml
kubectl get configmap app-config -n app-prod -o yaml

# Check ConfigMirror status
kubectl describe configmirror test-mirror -n configmirror-system
```

Expected status should show:
- `Ready: True`
- `Database Status: Connected: true`
- `Replicated Config Maps` listing the ConfigMap

## Edge Case Tests

### Test 1: Source ConfigMap Deletion

When the source ConfigMap is deleted, replicas should be automatically cleaned up.

```bash
# Delete source
kubectl delete configmap app-config -n app-source

# Wait a few seconds for reconciliation
sleep 5

# Verify replicas are gone
kubectl get configmap app-config -n app-staging  # Should return NotFound
kubectl get configmap app-config -n app-prod     # Should return NotFound
```

### Test 2: Label Change

When a ConfigMap's labels no longer match the selector, replicas should be removed.

```bash
# Recreate the ConfigMap
kubectl apply -f test-configmap.yaml

# Wait for replication
sleep 5

# Verify replicas exist
kubectl get configmap app-config -n app-staging

# Change label so it no longer matches
kubectl label configmap app-config -n app-source replicate=false --overwrite

# Wait for reconciliation
sleep 5

# Verify replicas are removed
kubectl get configmap app-config -n app-staging  # Should return NotFound
kubectl get configmap app-config -n app-prod     # Should return NotFound

# Source should still exist
kubectl get configmap app-config -n app-source   # Should still exist
```

### Test 3: ConfigMirror CR Deletion

When the ConfigMirror CR is deleted, the finalizer should clean up all replicas but leave the source untouched.

```bash
# Re-enable replication
kubectl label configmap app-config -n app-source replicate=true --overwrite

# Wait for replication
sleep 5

# Verify replicas exist
kubectl get configmap app-config -n app-staging
kubectl get configmap app-config -n app-prod

# Delete the ConfigMirror CR
kubectl delete configmirror test-mirror -n configmirror-system

# Wait for finalizer to complete
sleep 10

# Verify replicas are removed
kubectl get configmap app-config -n app-staging  # Should return NotFound
kubectl get configmap app-config -n app-prod     # Should return NotFound

# Source should still exist (not managed by the operator)
kubectl get configmap app-config -n app-source   # Should still exist
```

## Database Persistence Verification

Create a pod to query the database. Create a file `psql-debug.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: psql-debug
  namespace: configmirror-system
spec:
  restartPolicy: Never
  containers:
  - name: postgres
    image: postgres:17
    command: ['sleep', '3600']
    env:
    - name: PGPASSWORD
      valueFrom:
        secretKeyRef:
          name: rds-credentials
          key: password
    - name: PGHOST
      valueFrom:
        secretKeyRef:
          name: rds-credentials
          key: host
    - name: PGDATABASE
      valueFrom:
        secretKeyRef:
          name: rds-credentials
          key: dbname
    - name: PGUSER
      valueFrom:
        secretKeyRef:
          name: rds-credentials
          key: username
```

Apply and query:

```bash
# Create pod
kubectl apply -f psql-debug.yaml

# Wait for pod to be ready
kubectl wait --for=condition=Ready pod/psql-debug -n configmirror-system --timeout=30s

# Query database
kubectl exec -n configmirror-system psql-debug -- psql -c "SELECT namespace, name, created_at FROM configmaps ORDER BY created_at DESC LIMIT 10;"

# Check full data
kubectl exec -n configmirror-system psql-debug -- psql -c "SELECT namespace, name, LENGTH(data::text) as data_size, created_at FROM configmaps;"

# Cleanup
kubectl delete pod psql-debug -n configmirror-system
```

## Automated Tests

The operator includes comprehensive automated tests:

```bash
# Run unit tests
make test

# Run E2E tests (requires Kind cluster)
make test-e2e

# Run all tests with coverage
make test-coverage
```

## Cleanup

```bash
# Delete test resources
kubectl delete configmap app-config -n app-source --ignore-not-found
kubectl delete configmirror test-mirror -n configmirror-system --ignore-not-found
kubectl delete namespace app-source app-staging app-prod --ignore-not-found
```
