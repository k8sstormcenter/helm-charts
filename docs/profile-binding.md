# Profile binding: what governs a container

The node agent judges every container against exactly one of two things:

1. an authored ContainerProfile (an SBoB) bound to the pod through the
   `kubescape.io/user-defined-profile` label, verified under the trust policy
   when one is mounted, or
2. the empty profile, when no authored profile is bound.

A learned profile (the shadow the agent records for every container) is
never used for judgement. It learns for the life of the container and is
the input for `bobctl` when an SBoB is composed from observed behaviour. The
same rule applies to the host: a node binds a host profile through the same
label on the Node object, otherwise the host is judged against the empty
profile.

## Consequences

- An unbound container alerts on its first exec, open, syscall, capability,
  DNS query and network peer, once per rule and distinct subject, and raises
  the rogue-artefact alert R1017 until it is bound.
- Binding an SBoB is therefore not optional for a quiet cluster. It is the
  mechanism that turns a rogue workload into a governed one.
- Learned profiles are never deleted by the agent and never become the
  governing profile, whatever status storage stamps on them.

## Silencing a workload you do not want to govern yet

Bind an allow-all profile. It is an ordinary authored profile whose every
surface is a wildcard, so no rule fires, while the rogue state heals because
the pod is bound. This is the intended escape hatch for workloads that are
noisy, third party, or not yet profiled. An admission controller such as
Kyverno can stamp the label on every pod in a namespace and create the
profile alongside it.

Wildcards per surface:

| surface | entry |
|---|---|
| execs | `{path: "*", args: ["⋯⋯"]}` |
| opens | `{path: "*"}` |
| capabilities | `"*"` |
| syscalls | `"*"` |
| egress, ingress addresses | `ipAddresses: ["*"]` with no `ports` |
| egress domains | `dnsNames: ["*", "*.*"]` |
| egress, ingress pod peers | a `podSelector` with a `matchExpressions` entry that every pod satisfies, a `namespaceSelector` that every namespace satisfies, no `ports` |
| host peer | `entity: host` |

Example, bound to every pod in the namespace carrying `app: legacy`:

```yaml
apiVersion: spdx.softwarecomposition.kubescape.io/v1beta1
kind: ContainerProfile
metadata:
  name: allow-all
  namespace: legacy
  annotations:
    kubescape.io/managed-by: User
spec:
  architectures: [amd64, arm64]
  capabilities: ["*"]
  syscalls: ["*"]
  execs:
  - path: "*"
    args: ["⋯⋯"]
  opens:
  - path: "*"
  egress:
  - identifier: any-address
    type: external
    ipAddresses: ["*"]
    dnsNames: ["*", "*.*"]
  - identifier: any-pod
    type: internal
    podSelector:
      matchExpressions:
      - {key: kubescape.io/never-set, operator: DoesNotExist}
    namespaceSelector:
      matchExpressions:
      - {key: kubernetes.io/metadata.name, operator: Exists}
  - identifier: node
    entity: host
  ingress:
  - identifier: any-address
    type: external
    ipAddresses: ["*"]
  - identifier: any-pod
    type: internal
    podSelector:
      matchExpressions:
      - {key: kubescape.io/never-set, operator: DoesNotExist}
    namespaceSelector:
      matchExpressions:
      - {key: kubernetes.io/metadata.name, operator: Exists}
  - identifier: node
    entity: host
```

```yaml
metadata:
  labels:
    kubescape.io/user-defined-profile: allow-all
```

The pod label names the profile. For a multi-container pod the agent first
looks for `<label>-<containerName>` and then for the bare name, so one
allow-all profile serves every container.

When a trust policy is mounted in enforce mode the profile must also be
signed by a signer the policy trusts; an unsigned allow-all profile is then
refused and the pod stays deny-all. In alert mode it governs and the agent
logs that an unsigned profile is in force.

## Settings that matter for detection from the first event

| value | effect |
|---|---|
| `capabilities.runtimeDetection` | must be `enable`; `disable` turns rule evaluation off entirely |
| `alertCRD.installDefault` | must be `true` for the default rules to be installed |
| `nodeAgent.config.denyAllAlertsOnOpens` | file-open rules stay silent for unbound containers unless this is true |
| `nodeAgent.config.maxLearningPeriod` | no longer gates judgement; it decides when the learned profile is first offered to the suggester and how long storage waits before it treats a silent series as gone |
