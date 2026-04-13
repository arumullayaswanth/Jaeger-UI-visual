# Deploy Guide

## Current Two-Service Deployment

Use this section for the current codebase.

The current app now has 2 microservices:

1. `checkout-service`
   This is the public service behind ingress.
2. `inventory-service`
   This is the internal service called by `checkout-service`.

Current trace flow:

1. browser -> `checkout-service`
2. `checkout-service` -> `inventory-service`
3. both services -> OpenTelemetry Collector
4. OpenTelemetry Collector -> Jaeger
5. Jaeger -> Elasticsearch -> EBS

Important:

- the older single-service steps below are now old
- use this section first

### Step 1: Build 2 Images

Build `checkout-service`:

```bash
docker build -t <your-checkout-ecr-image> -f app/checkout-service/Dockerfile app
docker push <your-checkout-ecr-image>
```

Build `inventory-service`:

```bash
docker build -t <your-inventory-ecr-image> -f app/inventory-service/Dockerfile app
docker push <your-inventory-ecr-image>
```

### Step 2: Update The 2 App Images

Update `manifests/app/checkout-service-deployment.yaml`

Change:

```yaml
image: "<your-checkout-ecr-image>"
```

Update `manifests/app/inventory-service-deployment.yaml`

Change:

```yaml
image: "<your-inventory-ecr-image>"
```

### Step 3: Update Ingress

Update `manifests/ingress/ingress.yaml`

Change:

```yaml
alb.ingress.kubernetes.io/certificate-arn: "<your-acm-certificate-arn>"
- host: "<your-jaeger-domain>"
- host: "<your-app-domain>"
```

The app domain points to `checkout-service`.

### Step 4: Deploy In Order

```bash
kubectl apply -f manifests/base/namespace.yaml
kubectl apply -f manifests/elasticsearch/storageclass.yaml
kubectl apply -f manifests/elasticsearch/headless-service.yaml
kubectl apply -f manifests/elasticsearch/service.yaml
kubectl apply -f manifests/elasticsearch/pdb.yaml
kubectl apply -f manifests/elasticsearch/statefulset.yaml
helm repo add jaegertracing https://jaegertracing.github.io/helm-charts
helm repo update
helm upgrade --install jaeger jaegertracing/jaeger --namespace observability --version 3.4.1 -f helm/jaeger-values.yaml
kubectl apply -f manifests/otel-collector/
kubectl apply -f manifests/app/
kubectl apply -f manifests/ingress/ingress.yaml
```

### Step 5: Check Both Services

```bash
kubectl -n observability rollout status deployment/checkout-service
kubectl -n observability rollout status deployment/inventory-service
kubectl -n observability get svc checkout-service
kubectl -n observability get svc inventory-service
kubectl -n observability get hpa
```

### Step 6: Test Public Service

Open:

```text
https://<your-app-domain>/
https://<your-app-domain>/work
```

`/work` is the main test because it makes `checkout-service` call `inventory-service`.

### Step 7: Test Internal Service

Port-forward it:

```bash
kubectl -n observability port-forward svc/inventory-service 8081:80
```

Then test:

```bash
curl http://localhost:8081/healthz
curl http://localhost:8081/
curl http://localhost:8081/reserve
```

### Step 8: Test End-To-End Tracing

Run traffic a few times:

```bash
curl https://<your-app-domain>/work
curl https://<your-app-domain>/work
curl https://<your-app-domain>/work
```

In Jaeger:

1. open `https://<your-jaeger-domain>`
2. search service `checkout-service`
3. search service `inventory-service`

You should see traces that cross both services.

### Step 9: Check Logs

```bash
kubectl -n observability logs deployment/checkout-service
kubectl -n observability logs deployment/inventory-service
kubectl -n observability logs deployment/otel-collector
```



This file shows the full deployment flow in very simple steps.

Think of this project like 4 blocks:

1. Jaeger stores and shows traces.
2. OpenTelemetry Collector receives traces from apps.
3. Your Go app sends traces to the collector.
4. Ingress gives you a browser URL to open the app and Jaeger UI.
