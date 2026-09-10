sendgrid-stats-exporter
===========

A Helm chart for [chatwork/sendgrid-stats-exporter](https://github.com/chatwork/sendgrid-stats-exporter).

## Installing the Chart

### Local

```
$ helm install --set 'secret.apiKey=secret' --set 'secret.username=username' sendgrid-stats-exporter ./
or
$ helm install -f examples/override.yaml sendgrid-stats-exporter ./
```

Use an existing secret instead of chart-managed credentials:

```
$ helm install --set secret.create=false --set secret.name=my-sendgrid-secret sendgrid-stats-exporter ./
```

### Remote

[helm-git](https://github.com/aslafy-z/helm-git) plugin is required.

```
$ helm repo add sendgrid-stats-exporter 'git+https://github.com/chatwork/sendgrid-stats-exporter@charts?ref=0.0.10'
$ helm install -f examples/override.yaml sendgrid-stats-exporter
```

### Test

```
$ kubectl get svc
NAME                      TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)    AGE
sendgrid-stats-exporter   ClusterIP   10.108.179.32   <none>        9154/TCP   2m54s

$ kubectl run -it --rm=true busybox --image=yauritux/busybox-curl --restart=Never
/home # curl 10.108.179.32:9154/-/healthy
OK
```

## Configuration

The following table lists the configurable parameters of the Sendgrid-stats-exporter chart and their default values.

| Parameter                | Description             | Default        |
| ------------------------ | ----------------------- | -------------- |
| `replicaCount` | Number of replicas | `1` |
| `image.repository` | Image repository | `"chatwork/sendgrid-stats-exporter"` |
| `image.pullPolicy` | Image pull policy | `"IfNotPresent"` |
| `image.tag` | Image tag | `"v0.0.10"` |
| `imagePullSecrets` | Image pull secret | `[]` |
| `nameOverride` | Override the name of the chart | `""` |
| `fullnameOverride` | Override the full-name of the chart | `""` |
| `serviceAccount.create` | If true, create a new service account | `true` |
| `serviceAccount.annotations` | Annotations for serviceAccount | `{}` |
| `serviceAccount.name` | Name of the service account | `""` |
| `podAnnotations` | Annotations for the pod | `{}` |
| `podSecurityContext` | Security context for the pod | non-root UID 65532 |
| `securityContext` | Security context for container | drop ALL, read-only root, non-root |
| `envFrom` | Extra environment from ConfigMaps/Secrets | `[]` |
| `extraEnv` | Extra environment variables (map). Set `SENDGRID_INCLUDE_SUBUSERS: "true"` to scrape monthly stats for all subusers. | `{}` |
| `secret.create` | If true, create a Secret from values | `true` |
| `secret.name` | Existing secret name when `secret.create=false` | `""` |
| `secret.apiKey` | SendGrid API token (used when creating a Secret) | `""` |
| `secret.username` | SendGrid username | `""` |
| `service.type` | Service Type | `"ClusterIP"` |
| `service.port` | Service port | `9154` |
| `ingress.enabled` | If true, enable Ingress | `false` |
| `ingress.className` | IngressClass name | `""` |
| `ingress.annotations` | Annotations for ingress | `{}` |
| `ingress.hosts` | Ingress hosts and paths | `chart-example.local` `/` Prefix |
| `ingress.tls` | Ingress TLS configuration | `[]` |
| `resources.limits.cpu` |  | `"200m"` |
| `resources.limits.memory` |  | `"256Mi"` |
| `resources.requests.cpu` |  | `"100m"` |
| `resources.requests.memory` |  | `"128Mi"` |
| `autoscaling.enabled` | Enable HPA. Not recommended; extra replicas multiply SendGrid API calls. | `false` |
| `autoscaling.minReplicas` |  | `1` |
| `autoscaling.maxReplicas` |  | `5` |
| `autoscaling.targetCPUUtilizationPercentage` |  | `80` |
| `serviceMonitor.enabled` | Create a Prometheus Operator ServiceMonitor | `false` |
| `serviceMonitor.interval` | Scrape interval | `"60s"` |
| `serviceMonitor.scrapeTimeout` | Scrape timeout (must stay below `interval`; use ≥ 3× `SENDGRID_TIMEOUT` with subusers) | `"45s"` |
| `nodeSelector` | Node labels for pod assignment | `{}` |
| `tolerations` | Add tolerations | `[]` |
| `affinity` | Node/Pod affinities | `{}` |
