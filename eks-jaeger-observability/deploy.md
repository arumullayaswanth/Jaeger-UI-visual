# Deploy Guide

This file shows the full deployment flow in very simple steps.

Think of this project like 4 blocks:

1. Jaeger stores and shows traces.
2. OpenTelemetry Collector receives traces from apps.
3. Your Go app sends traces to the collector.
4. Ingress gives you a browser URL to open the app and Jaeger UI.

## Before You Start

You need these things ready first:

- an AWS EKS cluster
- `kubectl` working for that cluster
- `helm` installed
- Docker installed
- an ECR repository for the sample app image
- AWS Load Balancer Controller already installed in EKS
- an ACM certificate for HTTPS
- Amazon EBS CSI driver installed in the EKS cluster


## Step 0: Check AWS Load Balancer Controller

This project uses Kubernetes Ingress with AWS ALB.
So the AWS Load Balancer Controller must exist in your EKS cluster.

Check it:

```bash
kubectl get deployment -n kube-system aws-load-balancer-controller
kubectl get pods -n kube-system | grep aws-load-balancer-controller
```

If you already see the deployment and pods, you are fine.

If it is not installed, here is an example install flow.

Set these values first:

```bash
export CLUSTER_NAME=my-eks-cluster
export AWS_REGION=us-east-1
export ACCOUNT_ID=123456789012
```

Associate IAM OIDC provider:

```bash
eksctl utils associate-iam-oidc-provider \
  --region $AWS_REGION \
  --cluster $CLUSTER_NAME \
  --approve
```

Download the IAM policy:

```bash
curl -O https://raw.githubusercontent.com/kubernetes-sigs/aws-load-balancer-controller/main/docs/install/iam_policy.json
```

Create the IAM policy:

```bash
aws iam create-policy \
  --policy-name AWSLoadBalancerControllerIAMPolicy \
  --policy-document file://iam_policy.json
```

Create the IAM service account:

```bash
eksctl create iamserviceaccount \
  --cluster $CLUSTER_NAME \
  --region $AWS_REGION \
  --namespace kube-system \
  --name aws-load-balancer-controller \
  --attach-policy-arn arn:aws:iam::$ACCOUNT_ID:policy/AWSLoadBalancerControllerIAMPolicy \
  --override-existing-serviceaccounts \
  --approve
```
Get your VPC ID

```bash
aws eks describe-cluster \
  --name $CLUSTER_NAME \
  --region $AWS_REGION \
  --query "cluster.resourcesVpcConfig.vpcId" \
  --output text
```
Example output:
```bash
vpc-0abc123def456ghi
```
Install the controller with Helm:
- 👉 Replace <YOUR_VPC_ID> with

```bash
helm repo add eks https://aws.github.io/eks-charts
helm repo update eks

helm upgrade --install aws-load-balancer-controller eks/aws-load-balancer-controller \
  -n kube-system \
  --set clusterName=$CLUSTER_NAME \
  --set serviceAccount.create=false \
  --set serviceAccount.name=aws-load-balancer-controller \
  --set region=$AWS_REGION \
  --set vpcId=<YOUR_VPC_ID>
```

Verify again:

```bash
kubectl get deployment -n kube-system aws-load-balancer-controller
```
Delete EKS ALB Controller
- Delete Helm Release (MAIN step)
```bash
helm uninstall aws-load-balancer-controller -n kube-system
```
- Delete Service Account (Kubernetes)
```bash
kubectl delete serviceaccount aws-load-balancer-controller -n kube-system
```
- Delete IAM Service Account (EKS / CloudFormation)
```bash
eksctl delete iamserviceaccount \
  --cluster $CLUSTER_NAME \
  --region $AWS_REGION \
  --namespace kube-system \
  --name aws-load-balancer-controller
```
- Delete IAM Policy (AWS)
```bash
aws iam delete-policy \
  --policy-arn arn:aws:iam::$ACCOUNT_ID:policy/AWSLoadBalancerControllerIAMPolicy
```
- Verify Cleanup
```bash
kubectl get deployment -n kube-system aws-load-balancer-controller
kubectl get pods -n kube-system | grep aws-load-balancer-controller
```



## Step 0.1: Create Or Check ACM Certificate For HTTPS

Your ALB Ingress uses HTTPS, so you need an ACM certificate.

Example:

- domain: `mydomain.com`
- Jaeger: `jaeger.mydomain.com`
- app: `tracing-demo.mydomain.com`

Request a public certificate:

```bash
aws acm request-certificate \
  --region us-east-1 \
  --domain-name jaeger.mydomain.com \
  --subject-alternative-names tracing-demo.mydomain.com \
  --validation-method DNS
```

This command returns a certificate ARN.

Check the certificate:

```bash
aws acm list-certificates --region us-east-1
```

Describe it:

```bash
aws acm describe-certificate \
  --region us-east-1 \
  --certificate-arn <your-certificate-arn>
```

Important:

- complete DNS validation in Route 53 or your DNS provider
- wait until ACM certificate status becomes `ISSUED`
- then put that certificate ARN in `manifests/ingress/ingress.yaml`

In this file:

- `manifests/ingress/ingress.yaml`

Change:

```yaml
alb.ingress.kubernetes.io/certificate-arn: arn:aws:acm:us-east-1:123456789012:certificate/replace-me
```

To your real ACM certificate ARN.

## Step 1: Open The Project Folder

Run:

```bash
cd "c:\Users\Yaswanth Reddy\OneDrive - vitap.ac.in\Desktop\Distributed Tracing with Jaeger\eks-jaeger-observability"
```

## Step 0.2: Check Amazon EBS CSI Driver

Elasticsearch will store data on EBS volumes.
So the EBS CSI driver must exist in your cluster.

Check it:

```bash
kubectl get pods -n kube-system | grep ebs-csi
```

If it is not installed, install it:

```bash
eksctl create addon \
  --name aws-ebs-csi-driver \
  --cluster $CLUSTER_NAME \
  --region $AWS_REGION \
  --force
```

Verify:

```bash
kubectl get pods -n kube-system | grep ebs-csi
```

## Step 2: Change The Fake Values

This project has placeholder values. Replace them before deployment.

Update these files:

- `values.yaml`
- `helm/jaeger-values.yaml`
- `manifests/elasticsearch/statefulset.yaml`
- `manifests/app/deployment.yaml`
- `manifests/app/serviceaccount.yaml`
- `manifests/otel-collector/serviceaccount.yaml`
- `manifests/ingress/ingress.yaml`

What to change:

- replace `123456789012` with your AWS account ID
- replace `us-east-1` with your AWS region
- replace `jaeger.example.com` with your real Jaeger DNS name
- replace `tracing-demo.example.com` with your real app DNS name
- replace `arn:aws:acm:...:certificate/replace-me` with your real ACM certificate ARN
- change EBS volume size in `manifests/elasticsearch/statefulset.yaml` if needed
- current EBS StorageClass name is `jaeger-elasticsearch-gp2`
- current EBS volume type is `gp2` in `manifests/elasticsearch/storageclass.yaml`
- replace the ECR app image in `manifests/app/deployment.yaml`

Easy example:

- if your real domain is `mydomain.com`
- then use `jaeger.mydomain.com` for Jaeger
- and use `tracing-demo.mydomain.com` for the sample app

## Step 3: Build The Go App Image

From the project root:

```bash
docker build -t <aws-account-id>.dkr.ecr.<region>.amazonaws.com/otel-sample-app:1.0.0 app
docker push <aws-account-id>.dkr.ecr.<region>.amazonaws.com/otel-sample-app:1.0.0
```

After pushing the image, make sure the same image tag is present in:

`manifests/app/deployment.yaml`

## Step 4: Create The Namespace

Run:

```bash
kubectl apply -f manifests/base/namespace.yaml
```

Check:

```bash
kubectl get ns observability
```

You should see the `observability` namespace.

## Step 5: Deploy Elasticsearch With EBS

Run:

```bash
kubectl apply -f manifests/elasticsearch/storageclass.yaml
kubectl apply -f manifests/elasticsearch/headless-service.yaml
kubectl apply -f manifests/elasticsearch/service.yaml
kubectl apply -f manifests/elasticsearch/pdb.yaml
kubectl apply -f manifests/elasticsearch/statefulset.yaml
```

Check:

```bash
kubectl -n observability get pods -l app.kubernetes.io/name=elasticsearch
kubectl -n observability get pvc
kubectl -n observability get svc elasticsearch
```

Wait until all Elasticsearch pods are `Running`.

Note:

- this setup currently uses EBS `gp2`
- if you want another storage type later, update:
  - `manifests/elasticsearch/storageclass.yaml`
  - `manifests/elasticsearch/statefulset.yaml`

## Step 5.1: Optional Grafana Jaeger Datasource

Run:

```bash
kubectl apply -f manifests/jaeger/grafana-datasource.yaml
```

## Step 6: Install Jaeger With Helm

Add the Helm repo:

```bash
helm repo add jaegertracing https://jaegertracing.github.io/helm-charts
helm repo update
```

Install Jaeger:

```bash
helm upgrade --install jaeger jaegertracing/jaeger ^
  --namespace observability ^
  --version 3.4.1 ^
  -f helm/jaeger-values.yaml
```

Check:

```bash
kubectl -n observability get pods
kubectl -n observability get svc
```

You should see Jaeger collector, query, and agent resources.

## Step 7: Deploy The OpenTelemetry Collector

Run:

```bash
kubectl apply -f manifests/otel-collector/
```

Check:

```bash
kubectl -n observability rollout status deployment/otel-collector
kubectl -n observability get svc otel-collector
kubectl -n observability get hpa otel-collector
```

The deployment should become ready.

## Step 8: Deploy The Sample App

Run:

```bash
kubectl apply -f manifests/app/
```

Check:

```bash
kubectl -n observability rollout status deployment/otel-sample-app
kubectl -n observability get svc otel-sample-app
kubectl -n observability get hpa otel-sample-app
```

The app should become ready.

## Step 9: Deploy The Ingress

Run:

```bash
kubectl apply -f manifests/ingress/ingress.yaml
```

Check:

```bash
kubectl -n observability get ingress
```

Wait until the ingress gets an address.

## Step 10: Point DNS To The ALB

After the ingress is created, get the ALB hostname:

```bash
kubectl -n observability get ingress observability-alb
```

Create DNS records:

- `jaeger.example.com` -> ALB DNS name
- `tracing-demo.example.com` -> ALB DNS name

Use your real hostnames, not the example names.

Example:

- domain name: `mydomain.com`
- Jaeger URL: `jaeger.mydomain.com`
- app URL: `tracing-demo.mydomain.com`

If your ALB DNS name is:

```text
k8s-observability-1234567890.us-east-1.elb.amazonaws.com
```

Then in Route 53 create:

1. Record for Jaeger
   Name: `jaeger`
   Type: `A`
   Alias: `Yes`
   Target: `k8s-observability-1234567890.us-east-1.elb.amazonaws.com`

2. Record for app
   Name: `tracing-demo`
   Type: `A`
   Alias: `Yes`
   Target: `k8s-observability-1234567890.us-east-1.elb.amazonaws.com`

So the final mapping becomes:

- `jaeger.mydomain.com` -> ALB
- `tracing-demo.mydomain.com` -> ALB

Also make sure you update this file before applying ingress:

- `manifests/ingress/ingress.yaml`

Change:

```yaml
host: jaeger.example.com
host: tracing-demo.example.com
```

To values like:

```yaml
host: jaeger.mydomain.com
host: tracing-demo.mydomain.com
```

## Step 11: Access The App

Open in browser:

```text
https://tracing-demo.example.com
```

You should get JSON like:

```json
{
  "message": "Distributed tracing is active",
  "service": "otel-sample-app",
  "traceId": "example-trace-id",
  "syntheticDelayMs": 100
}
```

Also test:

```text
https://tracing-demo.example.com/work
https://tracing-demo.example.com/healthz
https://tracing-demo.example.com/readyz
```

## Step 12: Access Jaeger UI

Open:

```text
https://jaeger.example.com
```

In Jaeger UI:

1. choose service `otel-sample-app`
2. click `Find Traces`
3. open one trace

You should see spans like:

- `business.root`
- `simulate.checkout.flow`
- `inventory.lookup`
- `payment.authorization`

## Step 13: Test From Terminal

Run these commands a few times:

```bash
curl https://tracing-demo.example.com/
curl https://tracing-demo.example.com/work
curl https://tracing-demo.example.com/work
curl https://tracing-demo.example.com/work
```

This creates traffic so traces appear in Jaeger.

## Step 14: Check Logs

App logs:

```bash
kubectl -n observability logs deployment/otel-sample-app
```

Collector logs:

```bash
kubectl -n observability logs deployment/otel-collector
```

Jaeger logs:

```bash
kubectl -n observability logs deployment/jaeger-query
kubectl -n observability logs deployment/jaeger-collector
```

## Step 15: If The App Does Not Work

Check pods:

```bash
kubectl -n observability get pods
```

Describe the failing pod:

```bash
kubectl -n observability describe pod <pod-name>
```

Check ingress:

```bash
kubectl -n observability describe ingress observability-alb
```

Check services:

```bash
kubectl -n observability get svc
```

Check if the app can send traces:

```bash
kubectl -n observability logs deployment/otel-sample-app
kubectl -n observability logs deployment/otel-collector
```

## Step 16: Quick Success Checklist

Your deployment is correct when all of these are true:

- namespace `observability` exists
- Jaeger pods are running
- OpenTelemetry Collector pods are running
- sample app pods are running
- ingress has an ALB address
- app URL opens in browser
- Jaeger URL opens in browser
- hitting `/work` creates traces
- you can search `otel-sample-app` in Jaeger

## One Simple Deploy Order To Remember

If you want the shortest memory trick, remember this order:

1. build image
2. create namespace
3. deploy Elasticsearch
4. install Jaeger
5. deploy collector
6. deploy app
7. deploy ingress
8. open app
9. open Jaeger
10. test traces

## Useful Commands Together

```bash
kubectl apply -f manifests/base/namespace.yaml
kubectl apply -f manifests/elasticsearch/storageclass.yaml
kubectl apply -f manifests/elasticsearch/headless-service.yaml
kubectl apply -f manifests/elasticsearch/service.yaml
kubectl apply -f manifests/elasticsearch/pdb.yaml
kubectl apply -f manifests/elasticsearch/statefulset.yaml
kubectl apply -f manifests/jaeger/grafana-datasource.yaml
helm upgrade --install jaeger jaegertracing/jaeger --namespace observability --version 3.4.1 -f helm/jaeger-values.yaml
kubectl apply -f manifests/otel-collector/
kubectl apply -f manifests/app/
kubectl apply -f manifests/ingress/ingress.yaml
kubectl -n observability get pods
kubectl -n observability get ingress
```

## If You Want To Remove Everything

```bash
helm uninstall jaeger -n observability
kubectl delete -f manifests/ingress/ingress.yaml
kubectl delete -f manifests/app/
kubectl delete -f manifests/otel-collector/
kubectl delete -f manifests/jaeger/grafana-datasource.yaml
kubectl delete -f manifests/elasticsearch/statefulset.yaml
kubectl delete -f manifests/elasticsearch/pdb.yaml
kubectl delete -f manifests/elasticsearch/service.yaml
kubectl delete -f manifests/elasticsearch/headless-service.yaml
kubectl delete -f manifests/elasticsearch/storageclass.yaml
kubectl delete -f manifests/base/namespace.yaml
```
