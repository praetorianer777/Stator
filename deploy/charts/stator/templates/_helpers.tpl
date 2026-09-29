{{/* Names and labels. */}}

{{- define "stator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "stator.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "stator.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "stator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: stator
{{- end -}}

{{- define "stator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "stator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "stator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "stator.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "stator.secretName" -}}
{{- default (printf "%s-secrets" (include "stator.fullname" .)) .Values.secrets.existingSecret -}}
{{- end -}}

{{- define "stator.image" -}}
{{- $img := index .root.Values.images .name -}}
{{- printf "%s:%s" $img.repository (default .root.Chart.AppVersion $img.tag) -}}
{{- end -}}

{{/*
Where the database is. The CNPG cluster and the bundled Postgres name their own
services, so the answer depends on which one the chart brought.
*/}}

{{- define "stator.cnpgName" -}}
{{- printf "%s-postgres" (include "stator.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Writes, migrations and admin work: the CNPG -rw service, which always points
at the current primary.
*/}}
{{- define "stator.dbHost" -}}
{{- if and .Values.cnpg.enabled .Values.postgresql.enabled -}}
{{- fail "cnpg.enabled and postgresql.enabled are both true. Turn one of them off: the workloads can only be pointed at one database." -}}
{{- end -}}
{{- if .Values.cnpg.enabled -}}
{{- printf "%s-rw" (include "stator.cnpgName" .) -}}
{{- else if .Values.postgresql.enabled -}}
{{- printf "%s-postgresql" (include "stator.fullname" .) -}}
{{- else -}}
{{- required "Set database.host, or enable cnpg or the bundled postgresql: the chart does not guess where the database is." .Values.database.host -}}
{{- end -}}
{{- end -}}

{{/*
Reads: the CNPG -ro service when there is a replica behind it. With one
instance -ro has no endpoints, so reads stay on -rw. Comma separated, because
a template can only return a string.
*/}}
{{- define "stator.replicaHosts" -}}
{{- $hosts := .Values.database.replicaHosts | default list -}}
{{- if and .Values.cnpg.enabled .Values.cnpg.readReplicas (gt (int (.Values.cnpg.spec.instances | default 1)) 1) -}}
{{- $hosts = append $hosts (printf "%s-ro" (include "stator.cnpgName" .)) -}}
{{- end -}}
{{- join "," $hosts -}}
{{- end -}}

{{- define "stator.ownerRole" -}}
{{- $role := .Values.database.ownerRole -}}
{{- if and .Values.cnpg.enabled (eq $role "postgres") -}}
{{- fail "database.ownerRole is postgres, which CNPG keeps for its own superuser. Leave database.ownerRole empty to get stator_owner, or set another name." -}}
{{- end -}}
{{- default (ternary "stator_owner" "postgres" .Values.cnpg.enabled) $role -}}
{{- end -}}

{{/*
Valkey: the bundled one, yours, or none. Read-your-writes positions have to be
shared by every api pod once reads can reach a replica, so more than one api
pod with replicas and no Valkey is refused rather than quietly inconsistent.
*/}}
{{- define "stator.valkeyHost" -}}
{{- if .Values.valkey.bundled -}}
{{- printf "%s-valkey" (include "stator.fullname" .) -}}
{{- else -}}
{{- .Values.valkey.host -}}
{{- end -}}
{{- end -}}

{{- define "stator.valkeyAuth" -}}
{{- if or .Values.valkey.bundled .Values.valkey.auth -}}true{{- end -}}
{{- end -}}

{{- define "stator.checkValkey" -}}
{{- if and (include "stator.replicaHosts" .) (gt (int .Values.api.replicas) 1) (not (include "stator.valkeyHost" .)) -}}
{{- fail "Reads go to replicas and api.replicas is above 1, so read-your-writes needs a Valkey all api pods share. Set valkey.host, or valkey.bundled for a trial." -}}
{{- end -}}
{{- end -}}

{{/*
The basic-auth Secrets holding one password each. CNPG reads them in exactly
this shape, and the bundled database and the Jobs read the same ones.
*/}}
{{- define "stator.credentialSecret" -}}
{{- $suffix := dict "dbOwner" "db-owner" "dbApp" "db-app" "dbAdmin" "db-admin" "valkey" "valkey" "secretKey" "secret-key" -}}
{{- default (printf "%s-%s" (include "stator.fullname" .root) (get $suffix .name)) (get .root.Values.secrets.names .name) -}}
{{- end -}}

{{/*
A password from its Secret, as an env var the URLs below expand with $(NAME).
Kubernetes only expands variables declared earlier in the same list.
*/}}
{{- define "stator.passwordEnv" -}}
- name: {{ .var }}
  valueFrom:
    secretKeyRef:
      name: {{ include "stator.credentialSecret" (dict "root" .root "name" .name) }}
      key: password
{{- end -}}

{{/* Generated passwords are alphanumeric, so they need no escaping here. */}}
{{- define "stator.dbURL" -}}
{{- $v := .root.Values.database -}}
{{- printf "postgres://%s:$(%s)@%s:%d/%s?sslmode=%s" .role .var .host (int $v.port) $v.name $v.sslmode -}}
{{- end -}}

{{- define "stator.valkeyEnv" -}}
{{- $host := include "stator.valkeyHost" . -}}
{{- if $host -}}
{{- $port := int .Values.valkey.port -}}
{{- $db := int .Values.valkey.db -}}
{{- if include "stator.valkeyAuth" . -}}
{{ include "stator.passwordEnv" (dict "root" . "name" "valkey" "var" "VALKEY_PASSWORD") }}
- name: STATOR_VALKEY_URL
  value: {{ printf "redis://:$(VALKEY_PASSWORD)@%s:%d/%d" $host $port $db | quote }}
{{- else -}}
- name: STATOR_VALKEY_URL
  value: {{ printf "redis://%s:%d/%d" $host $port $db | quote }}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Annotations that order a hook the same way under Helm and under Argo CD. Each
tool reads only the half meant for it.
*/}}
{{- define "stator.hookAnnotations" -}}
{{- if .helm -}}
helm.sh/hook: {{ .helm }}
helm.sh/hook-weight: {{ .weight | quote }}
helm.sh/hook-delete-policy: before-hook-creation
{{- end }}
{{- if .argo }}
argocd.argoproj.io/hook: {{ .argo }}
argocd.argoproj.io/hook-delete-policy: BeforeHookCreation
{{- end }}
argocd.argoproj.io/sync-wave: {{ .weight | quote }}
{{- end -}}

{{- define "stator.appURL" -}}
{{- printf "%s://%s" (ternary "https" "http" .Values.ingress.tls.enabled) .Values.ingress.host -}}
{{- end -}}

{{/*
The environment every workload shares, built once so a setting cannot reach
the api and not the worker: they load the same configuration. Credentials are
not here; stator.secretEnv hands them over by reference.
*/}}
{{- define "stator.env" -}}
STATOR_ENV: {{ .Values.env | quote }}
STATOR_LOG_LEVEL: {{ .Values.logLevel | quote }}
STATOR_APP_URL: {{ include "stator.appURL" . | quote }}
STATOR_HTTP_ADDR: {{ printf ":%d" (int .Values.api.port) | quote }}
STATOR_METRICS_ADDR: {{ printf ":%d" (int .Values.telemetry.metricsPort) | quote }}
{{- with .Values.telemetry.otel.endpoint }}
STATOR_OTEL_ENDPOINT: {{ . | quote }}
STATOR_OTEL_SAMPLE_RATIO: {{ $.Values.telemetry.otel.sampleRatio | quote }}
{{- end }}
{{- with .Values.network.corsOrigins }}
STATOR_CORS_ORIGINS: {{ join "," . | quote }}
{{- end }}
STATOR_SESSION_COOKIE: {{ .Values.auth.sessionCookie | quote }}
STATOR_SECURE_COOKIES: {{ .Values.auth.secureCookies | quote }}

# The pools. The URLs carry credentials and come from the Secrets.
STATOR_DB_PRIMARY_MAX_CONNS: {{ .Values.database.pool.primaryMaxConns | quote }}
STATOR_DB_REPLICA_MAX_CONNS: {{ .Values.database.pool.replicaMaxConns | quote }}
STATOR_DB_MIN_CONNS: {{ .Values.database.pool.minConns | quote }}
STATOR_DB_CONN_MAX_LIFETIME: {{ .Values.database.pool.maxConnLifetime | quote }}
STATOR_DB_HEALTH_INTERVAL: {{ .Values.database.pool.healthInterval | quote }}
STATOR_DB_MAX_REPLICA_LAG: {{ .Values.database.pool.maxReplicaLag | quote }}
STATOR_DB_REPLICA_LAG_SAMPLES: {{ .Values.database.pool.replicaLagSamples | quote }}
STATOR_READ_YOUR_WRITES_TTL: {{ .Values.database.readYourWritesTTL | quote }}

{{- if .Values.s3.enabled }}
STATOR_S3_ENDPOINT: {{ required "Set s3.endpoint, the host and port of the bucket's S3 API, or turn s3.enabled off." .Values.s3.endpoint | quote }}
STATOR_S3_BUCKET: {{ .Values.s3.bucket | quote }}
STATOR_S3_REGION: {{ .Values.s3.region | quote }}
STATOR_S3_USE_SSL: {{ .Values.s3.useSSL | quote }}
{{- end }}
{{- end -}}

{{/* The credentials, by reference. Never rendered into a pod spec. */}}
{{- define "stator.secretEnv" -}}
{{- include "stator.checkValkey" . -}}
{{- $v := .Values.database -}}
{{- $host := include "stator.dbHost" . -}}
{{ include "stator.passwordEnv" (dict "root" . "name" "dbApp" "var" "DB_APP_PASSWORD") }}
{{ include "stator.passwordEnv" (dict "root" . "name" "dbAdmin" "var" "DB_ADMIN_PASSWORD") }}
{{ include "stator.passwordEnv" (dict "root" . "name" "secretKey" "var" "STATOR_SECRET_KEY") }}
- name: STATOR_DB_PRIMARY_URL
  value: {{ include "stator.dbURL" (dict "root" . "role" $v.appRole "var" "DB_APP_PASSWORD" "host" $host) | quote }}
- name: STATOR_DB_ADMIN_URL
  value: {{ include "stator.dbURL" (dict "root" . "role" $v.adminRole "var" "DB_ADMIN_PASSWORD" "host" $host) | quote }}
{{- with include "stator.replicaHosts" . }}
- name: STATOR_DB_REPLICA_URLS
  {{- $urls := list }}
  {{- range splitList "," . }}
  {{- $urls = append $urls (include "stator.dbURL" (dict "root" $ "role" $v.appRole "var" "DB_APP_PASSWORD" "host" .)) }}
  {{- end }}
  value: {{ join "," $urls | quote }}
{{- end }}
{{- with include "stator.valkeyEnv" . }}
{{ . }}
{{- end }}
{{- if .Values.s3.enabled }}
- name: STATOR_S3_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "stator.secretName" . }}
      key: s3-access-key
- name: STATOR_S3_SECRET_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "stator.secretName" . }}
      key: s3-secret-key
{{- end }}
{{- end -}}
