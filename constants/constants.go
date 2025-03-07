/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package constants

import "time"

// Command-line flag defaults
const (
	DefaultVersion    = "3.5.1"
	DefaultInstallDir = "/opt/bin/"

	DefaultReleaseURL      = "https://github.com/coreos/etcd/releases/download"
	DefaultImageRepository = "quay.io/coreos/etcd"
	DefaultCertificateDir  = "/etc/etcd/pki"

	UnitFile        = "/etc/systemd/system/etcd.service"
	EnvironmentFile = "/etc/etcd/etcd.env"
	EtcdctlEnvFile  = "/etc/etcd/etcdctl.env"

	DefaultDataDir    = "/var/lib/etcd"
	DefaultPodSpecDir = "/etc/kubernetes/manifests"

	DefaultLoopbackHost = "127.0.0.1"
	DefaultPeerPort     = 2380
	DefaultClientPort   = 2379

	DefaultDownloadConnectTimeout = 10 * time.Second
	DefaultEtcdRequestTimeout     = 5 * time.Second

	EtcdHealthCheckKey = "health"

	// EtcdCACertAndKeyBaseName defines etcd's CA certificate and key base name
	EtcdCACertAndKeyBaseName = "ca"
	// EtcdCACertName defines etcd's CA certificate name
	EtcdCACertName = "ca.crt"

	// EtcdServerCertAndKeyBaseName defines etcd's server certificate and key base name
	EtcdServerCertAndKeyBaseName = "server"
	// EtcdServerCertName defines etcd's server certificate name
	EtcdServerCertName = "server.crt"
	// EtcdServerKeyName defines etcd's server key name
	EtcdServerKeyName = "server.key"

	// EtcdPeerCertAndKeyBaseName defines etcd's peer certificate and key base name
	EtcdPeerCertAndKeyBaseName = "peer"
	// EtcdPeerCertName defines etcd's peer certificate name
	EtcdPeerCertName = "peer.crt"
	// EtcdPeerKeyName defines etcd's peer key name
	EtcdPeerKeyName = "peer.key"

	// APIServerEtcdClientCertAndKeyBaseName defines apiserver's etcd client certificate and key base name
	APIServerEtcdClientCertAndKeyBaseName = "apiserver-etcd-client"

	// EtcdctlClientCertAndKeyBaseName defines etcdctl's client certificate and key base name
	EtcdctlClientCertAndKeyBaseName = "etcdctl-etcd-client"
	// EtcdctllientCertName defines etcdctl's client certificate name
	EtcdctlClientCertName = "etcdctl-etcd-client.crt"
	// EtcdctlClientKeyName defines etcdctl's client key name
	EtcdctlClientKeyName = "etcdctl-etcd-client.key"

	// MastersGroup defines the well-known group for the apiservers. This group is also superuser by default
	// (i.e. bound to the cluster-admin ClusterRole)
	MastersGroup = "system:masters"

	UnitFileTemplate = `[Unit]
Description=etcd
Documentation=https://github.com/coreos/etcd
Conflicts=etcd-member.service
Conflicts=etcd2.service

[Service]
EnvironmentFile={{ .EnvironmentFile }}
ExecStart={{ .EtcdExecutable }}

Type=notify
TimeoutStartSec=0
Restart=on-failure
RestartSec=5s

LimitNOFILE=65536
{{- range .EtcdDiskPriorities }}
{{ . }}
{{- end }}
MemoryLow=200M

[Install]
WantedBy=multi-user.target
`

	EnvFileTemplate = `ETCD_NAME={{ .Name }}

# Initial cluster configuration
ETCD_INITIAL_CLUSTER={{ .InitialCluster }}
{{ if .InitialClusterToken }}ETCD_INITIAL_CLUSTER_TOKEN={{ .InitialClusterToken }}{{ end }}
ETCD_INITIAL_CLUSTER_STATE={{ .InitialClusterState }}

# Peer configuration
ETCD_INITIAL_ADVERTISE_PEER_URLS={{ .InitialAdvertisePeerURLs.String }}
ETCD_LISTEN_PEER_URLS={{ .ListenPeerURLs.String }}

ETCD_CLIENT_CERT_AUTH=true
ETCD_PEER_CERT_FILE={{ .PeerCertFile }}
ETCD_PEER_KEY_FILE={{ .PeerKeyFile }}
ETCD_PEER_TRUSTED_CA_FILE={{ .PeerTrustedCAFile }}

# Client/server configuration
ETCD_ADVERTISE_CLIENT_URLS={{ .AdvertiseClientURLs.String }}
ETCD_LISTEN_CLIENT_URLS={{ .ListenClientURLs.String }}

ETCD_PEER_CLIENT_CERT_AUTH=true
ETCD_CERT_FILE={{ .CertFile }}
ETCD_KEY_FILE={{ .KeyFile }}
ETCD_TRUSTED_CA_FILE={{ .TrustedCAFile }}

# Other
ETCD_DATA_DIR={{ .DataDir }}
ETCD_STRICT_RECONFIG_CHECK=true
{{ if .GOMAXPROCS }}GOMAXPROCS={{ .GOMAXPROCS }}{{ end }}
{{ if .EnableV2 }}ETCD_ENABLE_V2={{ .EnableV2 }}{{ end }}

# Logging configuration
{{ if .Logger }}ETCD_LOGGER={{ .Logger }}{{ end }}
{{ if .LogOutputs }}ETCD_LOG_OUTPUTS={{ .LogOutputs }}{{ end }}

# Profiling/metrics
{{ if .ListenMetricsURLs.String }}ETCD_LISTEN_METRICS_URLS={{ .ListenMetricsURLs.String }}{{ end }}
`

	EtcdctlEnvFileTemplate = `export ETCDCTL_API=3

export ETCDCTL_CACERT={{ .TrustedCAFile }}
export ETCDCTL_CERT={{ .EtcdctlCertFile }}
export ETCDCTL_KEY={{ .EtcdctlKeyFile }}

export ETCDCTL_DIAL_TIMEOUT=3s
`
	DefaultSkipRemoveMember = false
	DefaultCacheBaseDir     = "/var/cache/etcdadm/"

	EtcdctlShellWrapperTemplate = `#!/usr/bin/env sh
if ! [ -r "{{ .EtcdctlEnvFile }}" ]; then
	echo "Unable to read the etcdctl environment file '{{ .EtcdctlEnvFile }}'. The file must exist, and this wrapper must be run as root."
	exit 1
fi
. "{{ .EtcdctlEnvFile }}"
"{{ .EtcdctlExecutable }}" "$@"
`

	DefaultBackOffSteps    = 5
	DefaultBackOffDuration = 2 * time.Second
	DefaultBackOffFactor   = 2.0
)

// DefaultEtcdDiskPriorities defines the default etcd disk priority.
var DefaultEtcdDiskPriorities = []string{
	"Nice=-10",
	"IOSchedulingClass=best-effort",
	"IOSchedulingPriority=2",
}
