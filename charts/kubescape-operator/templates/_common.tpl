{{/* validate alertCRD.scopeClustered and alertCRD.scopeNamespaced are mutual exclusive */}}
{{- if and .Values.alertCRD.scopeClustered .Values.alertCRD.scopeNamespaced }}
{{- fail "alertCRD.scopeClustered and alertCRD.scopeNamespaced cannot both be true" }}
{{- end }}

{{- define "checksums" -}}
capabilitiesConfig: {{ include (printf "%s/%s/%s" $.Template.BasePath $.Values.global.configMapsDirectory "components-configmap.yaml") . | replace .Chart.AppVersion "" | replace .Chart.Version "" | sha256sum }}
cloudConfig: {{ include (printf "%s/%s/%s" $.Template.BasePath $.Values.global.configMapsDirectory "cloudapi-configmap.yaml") . | replace .Chart.AppVersion "" | replace .Chart.Version "" | sha256sum }}
cloudSecret: {{ include (printf "%s/%s/%s" $.Template.BasePath $.Values.global.configMapsDirectory "cloud-secret.yaml" ) . | replace .Chart.AppVersion "" | replace .Chart.Version "" | sha256sum }}
nodeAgentConfig: {{ include (printf "%s/node-agent/configmap.yaml" $.Template.BasePath) . | replace .Chart.AppVersion "" | replace .Chart.Version "" | sha256sum }}
operatorConfig: {{ include (printf "%s/operator/configmap.yaml" $.Template.BasePath) . | replace .Chart.AppVersion "" | replace .Chart.Version "" | sha256sum }}
proxySecret: {{ include (printf "%s/%s/%s" $.Template.BasePath $.Values.global.proxySecretDirectory "proxy-secret.yaml") . | replace .Chart.AppVersion "" | replace .Chart.Version "" | sha256sum }}
storageConfig: {{ include (printf "%s/storage/configmap.yaml" $.Template.BasePath) . | replace .Chart.AppVersion "" | replace .Chart.Version "" | sha256sum }}
admissionCertgenScripts: {{ include (printf "%s/operator/admission-webhook/configmap.yaml" $.Template.BasePath) . | replace .Chart.AppVersion "" | replace .Chart.Version "" | sha256sum }}
storageCertgenScripts: {{ include (printf "%s/storage/certgen/configmap.yaml" $.Template.BasePath) . | replace .Chart.AppVersion "" | replace .Chart.Version "" | sha256sum }}
{{- end -}}


{{- define "configurations" -}}
runtimeObservability: {{ eq .Values.capabilities.runtimeObservability "enable" }}
{{- end -}}

{{- define "components" -}}
nodeAgent:
  enabled: {{ or
   (eq .Values.capabilities.runtimeObservability "enable")
   (eq .Values.capabilities.networkPolicyService "enable")
   (eq .Values.capabilities.runtimeDetection "enable")
   (eq .Values.capabilities.malwareDetection "enable")
   (eq .Values.capabilities.seccompProfileService "enable")
  }}
operator:
  enabled: {{ eq .Values.capabilities.operator "enable" }}
storage:
  enabled: {{ .Values.storage.enabled }}
cloudSecret:
  name: cloud-secret
clamAV:
  enabled: {{ eq .Values.capabilities.malwareDetection "enable" }}
sbomScanner:
  enabled: {{ and (eq .Values.capabilities.nodeSbomGeneration "enable") .Values.nodeAgent.sbomScanner.enabled }}
customCaCertificates:
  name: custom-ca-certificates
autoUpdater:
  enabled: {{ eq .Values.capabilities.autoUpgrading "enable" }}
{{- end -}}

{{/*
"capabilities.gates" is the single source of truth for capability flags that are
silently ANDed with an internal precondition before they reach a rendered
manifest. The node-agent configmap consumes the effective.* values, while the
ks-capabilities configmap and NOTES.txt consume effectiveCapabilities/warnings.
Because the gate logic lives here only, the rendered value and the warning about
that value can never drift apart. See issue #851.
*/}}
{{- define "capabilities.gates" -}}
{{- $c := .Values.capabilities -}}
{{- $configurations := fromYaml (include "configurations" .) -}}
{{- $runtimeDetection := eq $c.runtimeDetection "enable" -}}
{{- $httpDetection := and (eq $c.httpDetection "enable") $runtimeDetection -}}
{{- $hostProfile := and (eq ($c.hostProfileService | default "disable") "enable") $configurations.runtimeObservability -}}
# effective.* are the node-agent config.json flags, consumed by node-agent/configmap.yaml
effective:
  httpDetectionEnabled: {{ $httpDetection }}
  hostProfileServiceEnabled: {{ $hostProfile }}
# effectiveCapabilities is requested-vs-effective per gated capability, consumed by ks-capabilities
effectiveCapabilities:
  httpDetection: {{ if $httpDetection }}enable{{ else }}disable{{ end }}
warnings:
{{- if and (eq $c.httpDetection "enable") (not $httpDetection) }}
- "capabilities.httpDetection=enable but capabilities.runtimeDetection is not enabled. HTTP detection runs on top of runtime detection, so httpDetectionEnabled renders as FALSE in the node-agent configmap. To use it, set capabilities.runtimeDetection=enable."
{{- end }}
{{- end -}}

{{- define "kubescape.certgen.scriptsHash" -}}
{{- printf "%s%s" (.Files.Get "scripts/certgen-create.sh") (.Files.Get "scripts/certgen-patch.sh") | sha256sum | trunc 8 -}}
{{- end }}

{{- define "kubescape.certificates.strategy" -}}
{{- $strategy := default "template" .Values.certificates.strategy -}}
{{- if not (has $strategy (list "template" "initContainer")) -}}
{{- fail (printf "certificates.strategy must be one of [template, initContainer], got %q" $strategy) -}}
{{- end -}}
{{- $strategy -}}
{{- end }}
