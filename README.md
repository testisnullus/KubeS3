# KubeS3
Seamlessly orchestrate your S3 buckets with a Kubernetes-native operator—no manual AWS console navigation required. Define and provision S3 resources via custom CRDs, leverage GitOps workflows, and let Kubernetes handle the heavy lifting for bucket creation, configuration, and lifecycle management.

## Prerequisites

- Kind cluster
- kubectl configured to access the cluster
- AWS credentials with S3 permissions

## Deployment Steps

### 1. Build and Deploy the Operator

Generate manifests and API code:

```bash
make manifests generate
```

Build and push the Docker image (replace the image registry):

```bash
IMG=danilamaster/s3:v1.13 make docker-build docker-push
```

Deploy the operator to your cluster:

```bash
IMG=danilamaster/s3:v1.13 make deploy
```

Monitor the deployment:

```bash
watch kubectl get pods -n kubes3-system
```

### 2. Create AWS Credentials Secret

**Important:** The operator requires AWS credentials to be provided via a Kubernetes Secret (can be refactored to IAM with SA), Create this secret before deploying any S3Bucket resources.

```bash
kubectl create secret generic aws-s3-secret \
  --from-literal=aws_access_key_id=YOUR_ACCESS_KEY \
  --from-literal=aws_secret_access_key=YOUR_SECRET_KEY \
  -n default
```

Or using a YAML manifest:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: aws-s3-secret
  namespace: default
type: Opaque
stringData:
  aws_access_key_id: YOUR_ACCESS_KEY
  aws_secret_access_key: YOUR_SECRET_KEY
```

### 3. Deploy an S3Bucket Resource

Create a custom resource to provision an S3 bucket:

```yaml
apiVersion: aws.nullzen.ai/v1
kind: S3Bucket
metadata:
  labels:
    app.kubernetes.io/name: kubes3
    app.kubernetes.io/managed-by: kustomize
  name: s3bucket-sample
  namespace: default
spec:
  awsCredsSecretRef:
    name: aws-s3-secret
    namespace: default
  bucketName: s3-operator-test-bucket-1
  region: eu-central-1
```

Apply the resource:

```bash
kubectl apply -f s3bucket-sample.yaml
```

### 4. Verify Deployment
View operator logs:

```bash
kubectl logs kubes3-controller-manager-<pod-id> -n kubes3-system
```

Check the status of your S3Bucket resource:

```bash
kubectl get s3buckets -n default
kubectl describe s3bucket s3bucket-sample -n default
```

## Leader Election

The operator supports leader election to ensure high availability when running multiple replicas. This prevents multiple controller instances from reconciling the same resources 
simultaneously.

### Enabling Leader Election

Leader election is disabled by default in the operator's main.go, but can be enabled manually:

```go
flag.BoolVar(&enableLeaderElection, "leader-elect", true,
    "Enable leader election for controller manager. "+
       "Enabling this will ensure there is only one active controller manager.")
```

### Verifying Leader Election

When leader election is active, you can inspect the lease object:

```bash
kubectl describe lease -n kubes3-system 1446e4db.nullzen.ai
```

Example output:

```
Name:         1446e4db.nullzen.ai
Namespace:    kubes3-system
Labels:       <none>
Annotations:  <none>
API Version:  coordination.k8s.io/v1
Kind:         Lease
Metadata:
  Creation Timestamp:  2025-10-07T16:07:41Z
  Resource Version:    2510
  UID:                 6c22e055-295d-4776-a585-f5e9a9dd556c
Spec:
  Acquire Time:            2025-10-07T16:12:20.937483Z
  Holder Identity:         kubes3-controller-manager-559f4b9797-2lc96_5e8fcc2a-9f76-4755-8270-28ba7a699f24
  Lease Duration Seconds:  15
  Lease Transitions:       1
  Renew Time:              2025-10-07T16:15:23.218012Z
Events:
  Type    Reason          Age    From                                                                             Message
  ----    ------          ----   ----                                                                             -------
  Normal  LeaderElection  7m43s  kubes3-controller-manager-ccd955886-865dq_04344356-319b-4dcd-b06b-1f038fc146c8   kubes3-controller-manager-ccd955886-865dq_04344356-319b-4dcd-b06b-1f038fc146c8 became leader
  Normal  LeaderElection  3m4s   kubes3-controller-manager-559f4b9797-2lc96_5e8fcc2a-9f76-4755-8270-28ba7a699f24  kubes3-controller-manager-559f4b9797-2lc96_5e8fcc2a-9f76-4755-8270-28ba7a699f24 became leader
```

**Key observations:**
- **Holder Identity**: Shows which pod currently holds the leader lock
- **Lease Transitions**: Indicates how many times leadership has changed (due to restarts or scaling)
- **Renew Time**: Last time the current leader renewed its lease
- **Events**: Historical record of leadership changes

### Running Multiple Replicas

To scale the operator for high availability:

```bash
kubectl scale deployment -n kubes3-system kubes3-controller-manager --replicas=3
```

Monitor the scaling:

```bash
watch kubectl get pods -n kubes3-system
```

Check logs from a specific replica:

```bash
kubectl logs -n kubes3-system kubes3-controller-manager-<pod-id>
```

View all leases to verify leader election:

```bash
kubectl get leases.coordination.k8s.io -A
```

Only one replica will actively reconcile resources, while others remain on standby. If the leader pod fails, another replica automatically becomes the leader within seconds.

## Cleanup

To remove an S3Bucket resource:

```bash
kubectl delete s3bucket s3bucket-sample -n default
```

To uninstall the operator:

```bash
make undeploy
```
