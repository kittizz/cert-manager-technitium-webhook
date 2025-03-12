# Cert-Manager Technitium Webhook

## Overview

The Cert-Manager Technitium Webhook is a DNS01 solver for cert-manager that allows you to request and renew SSL certificates using Technitium DNS Server for domain ownership verification through DNS challenges.

## Features

- Works with cert-manager to automatically issue certificates
- Supports Technitium DNS Server for DNS01 challenges
- Automatic zone detection
- Configurable TTL for TXT records

## Installation

### Prerequisites

- Kubernetes cluster
- Cert-Manager (v1.0.0+)
- Technitium DNS Server accessible from the webhook

### Install with Helm

```bash
# Add Helm repository
helm repo add kittizz https://kittizz.github.io/cert-manager-technitium-webhook
helm repo update

# Install webhook in the cert-manager namespace
helm install -n cert-manager cert-manager-technitium-webhook kittizz/cert-manager-technitium-webhook
```

## Configuration

### 1. Create API Token in Technitium DNS Server

1. Login to your Technitium DNS Server
2. Go to "Settings" > "API"
3. Create an API Token and save it

### 2. Create Secret for API Token

Create a `secret.yaml` file:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: technitium-api-token
  namespace: cert-manager
type: Opaque
stringData:
  api-token: your-technitium-api-token
```

Apply it:

```bash
kubectl apply -f secret.yaml
```

### 3. Create ClusterIssuer or Issuer

Create a `cluster-issuer.yaml` file:

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: technitium-letsencrypt
  namespace: cert-manager
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    # Or use staging server for testing
    # server: https://acme-staging-v02.api.letsencrypt.org/directory
    email: your-email@example.com
    privateKeySecretRef:
      name: acme-letsencrypt-key-prod
    solvers:
    - dns01:
        webhook:
          groupName: acme.xver.cloud
          solverName: technitium
          config:
            serverUrl: https://your-technitium-dns-server
            authTokenSecretRef:
              key: api-token
              name: technitium-api-token

```

Apply it:

```bash
kubectl apply -f cluster-issuer.yaml
```

### 4. Additional Cert-Manager Configuration (Recommended)

If you want cert-manager to use specific nameservers for DNS record verification, you may add the following arguments when installing cert-manager:

```bash
--set 'extraArgs={--dns01-recursive-nameservers-only,--dns01-recursive-nameservers=8.8.8.8:53\,1.1.1.1:53}'
```

## Usage

### Request a Certificate with Cert-Manager

Create a `certificate.yaml` file:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: example-com
  namespace: default
spec:
  secretName: example-com-tls
  dnsNames:
  - example.com
  - *.example.com
  issuerRef:
    name: technitium-letsencrypt
    kind: ClusterIssuer
```

Apply it:

```bash
kubectl apply -f certificate.yaml
```

## Troubleshooting

### Check webhook logs

```bash
kubectl logs -n cert-manager -l app=cert-manager-technitium-webhook
```

### Check Certificate status

```bash
kubectl describe certificate example-com
```

### Check Challenge status

```bash
kubectl get challenges -n default
kubectl describe challenge <challenge-name>
```

## Configuration Parameters

| Parameter          | Description                              | Default | Required |
| ------------------ | ---------------------------------------- | ------- | -------- |
| serverUrl          | Technitium DNS Server URL                | -       | Yes      |
| authTokenSecretRef | Reference to Secret containing API token | -       | Yes      |


## More Information

GitHub: [https://github.com/kittizz/cert-manager-technitium-webhook](https://github.com/kittizz/cert-manager-technitium-webhook)

Documentation: [https://kittizz.github.io/cert-manager-technitium-webhook/](https://kittizz.github.io/cert-manager-technitium-webhook/)

## Limitations

- This webhook requires access to the Technitium DNS Server API
- The API Token must have permissions to add/modify/delete DNS records