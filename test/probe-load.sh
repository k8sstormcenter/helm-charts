#!/bin/bash
# A/B for the node-agent health probes under a file-access event flood.
#   probe-load.sh baseline   installs the RELEASED duckling19 chart (1s/3s probes)
#   probe-load.sh fixed      installs the LOCAL fork chart (5s/10s probes)
#   probe-load.sh load       starts the noisy file walker on node-01
#   probe-load.sh watch      prints restarts per node-agent every 30s
#   probe-load.sh stop       removes the load pod
set -u
NS=kubescape
CHART_LOCAL=${CHART_LOCAL:-$HOME/helm-charts/charts/kubescape-operator}
case ${1:?mode} in
baseline|fixed)
  if [ "$1" = baseline ]; then
    helm repo add release https://k8sstormcenter.github.io/helm-charts --force-update >/dev/null && helm repo update >/dev/null
    SRC="release/kubescape-operator --version 1.41.0-duckling19"
  else
    SRC="$CHART_LOCAL"
  fi
  helm upgrade --install kubescape $SRC -n $NS --create-namespace --wait --timeout 10m \
    --set clusterName="$(kubectl config current-context)" \
    --set imagePullSecrets=duckling-pull \
    --set imagePullSecret.server=docker.io \
    --set imagePullSecret.username="$DOCKERHUB_USERNAME" \
    --set imagePullSecret.password="$DOCKERHUB_PAT" \
    --set alertCRD.installDefault=true \
    --set capabilities.runtimeDetection=enable \
    --set nodeAgent.config.alertManagerExporterUrls=null \
    --set nodeAgent.config.stdoutExporter=true \
    --set nodeAgent.config.maxLearningPeriod=2m \
    --set nodeAgent.config.learningPeriod=1m \
    --set nodeAgent.config.updatePeriod=30s >/dev/null
  kubectl -n $NS rollout status ds/node-agent --timeout=300s
  echo "probes now:"
  kubectl -n $NS get ds node-agent -o jsonpath='{range .spec.template.spec.containers[0]}  liveness  timeout={.livenessProbe.timeoutSeconds} period={.livenessProbe.periodSeconds} failures={.livenessProbe.failureThreshold}{"\n"}  readiness timeout={.readinessProbe.timeoutSeconds} period={.readinessProbe.periodSeconds}{"\n"}  startup   timeout={.startupProbe.timeoutSeconds} period={.startupProbe.periodSeconds}{"\n"}{end}'
  kubectl -n $NS get pods -l app=node-agent -o wide --no-headers | awk '{print "  "$1, $3, "restarts="$4, "node="$7}'
  ;;
load)
  kubectl create ns noisy --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  cat <<Y | kubectl apply -f - >/dev/null
apiVersion: v1
kind: Pod
metadata: { name: filewalker, namespace: noisy, labels: { app: filewalker } }
spec:
  nodeSelector: { kubernetes.io/hostname: node-01 }
  hostPID: false
  containers:
  - name: walker
    image: docker.io/library/debian:12-slim
    securityContext: { privileged: true }
    command: ["/bin/sh","-c"]
    args:
    - |
      while true; do
        find /host -xdev -type f 2>/dev/null | head -20000 | while read f; do head -c 1 "\$f" >/dev/null 2>&1; done
      done
    volumeMounts: [{ name: host, mountPath: /host, readOnly: true }]
  volumes: [{ name: host, hostPath: { path: /var/lib/rancher } }]
Y
  kubectl -n noisy wait --for=condition=Ready pod/filewalker --timeout=120s && echo "load running on node-01"
  ;;
watch)
  end=$((SECONDS+${2:-600}))
  while [ $SECONDS -lt $end ]; do
    echo "$(date +%T) $(kubectl -n $NS get pods -l app=node-agent --no-headers -o wide | awk '{printf "%s restarts=%s node=%s ready=%s | ", $1, $4, $7, $2}')"
    sleep 30
  done
  echo "--- probe failures seen:"
  for p in $(kubectl -n $NS get pods -l app=node-agent -o name); do
    echo "$p: $(kubectl -n $NS describe $p | grep -c 'failed liveness probe') liveness failures, $(kubectl -n $NS describe $p | grep -c 'context deadline exceeded') deadline exceeded"
  done
  ;;
stop)
  kubectl delete ns noisy --wait=false >/dev/null 2>&1; echo "load removed"
  ;;
esac
