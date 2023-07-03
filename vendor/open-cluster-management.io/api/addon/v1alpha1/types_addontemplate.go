package v1alpha1

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	work "open-cluster-management.io/api/work/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Cluster"
// +kubebuilder:printcolumn:name="ADDON NAME",type=string,JSONPath=`.spec.addonName`

// AddOnTemplate is the Custom Resource object, it is used to describe
// how to deploy the addon agent and how to register the addon.
//
// AddOnTemplate is a cluster-scoped resource, and will only be used
// on the hub cluster.
type AddOnTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// spec holds the registration configuration for the addon and the
	// addon agent resources yaml description.
	// +kubebuilder:validation:Required
	// +required
	Spec AddOnTemplateSpec `json:"spec"`
}

// AddOnTemplateSpec defines the template of an addon agent which will be deployed on managed clusters.
type AddOnTemplateSpec struct {
	// AddonName represents the name of the addon which the template belongs to
	// +kubebuilder:validation:Required
	// +required
	AddonName string `json:"addonName"`

	// AgentSpec describes what/how the kubernetes resources of the addon agent to be deployed on a managed cluster.
	// +kubebuilder:validation:Required
	// +required
	AgentSpec work.ManifestWorkSpec `json:"agentSpec"`

	// Registration holds the registration configuration for the addon
	// +optional
	Registration []RegistrationSpec `json:"registration"`

	// HealthProber defines how is the healthiness status of the ManagedClusterAddon probed.
	// Note that the prescribed prober type here only applies to the automatically installed
	// addons configured via InstallStrategy.
	// If nil, will be defaulted to "Lease" type.
	// +optional
	HealthProber *HealthProber `json:"healthProber,omitempty"`
}

type HealthProber struct {
	Type HealthProberType
}

type HealthProberType string

const (
	// HealthProberTypeNone indicates the healthiness status will be refreshed, which is
	// leaving the healthiness of ManagedClusterAddon to an empty string.
	HealthProberTypeNone HealthProberType = "None"
	// HealthProberTypeLease indicates the healthiness of the addon is connected with the
	// corresponding lease resource in the cluster namespace with the same name as the addon.
	// Note that the lease object is expected to periodically refresh by a local agent
	// deployed in the managed cluster implementing lease.LeaseUpdater interface.
	HealthProberTypeLease HealthProberType = "Lease"
	// // HealthProberTypeWork indicates the healthiness of the addon is equal to the overall
	// // dispatching status of the corresponding ManifestWork resource.
	// // It's applicable to those addons that don't have a local agent instance in the managed
	// // clusters. The addon framework will check if the work is Available on the spoke. In addition
	// // user can define a prober to check more detailed status based on status feedback from work.
	// HealthProberTypeWork HealthProberType = "Work"

	// HealthProberTypeAgentDeploymentAvailable indicates the healthiness of the addon is equal to the
	// overall dispatching status of the corresponding agent deployment resource.
	// For this type, the addon agent should be deployed as a deployment resource on the managed cluster.
	// The addon framework/manager will check if the deployment is Available on the spoke cluster.
	HealthProberTypeAgentDeploymentAvailable HealthProberType = "AgentDeploymentAvailable"
)

// RegistrationType represents the type of the registration configuration,
// it could be KubeClient or CustomSigner
type RegistrationType string

// HubPermissionsBindingType represent how to bind permission resources(role/clusterrole)
// on the hub cluster for the addon agent
type HubPermissionsBindingType string

const (
	// RegistrationTypeKubeClient represents the KubeClient type registration of the addon agent.
	// For this type, the addon agent can access the hub kube apiserver with kube style API.
	// The signer name should be "kubernetes.io/kube-apiserver-client".
	RegistrationTypeKubeClient RegistrationType = "KubeClient"
	// RegistrationTypeCustomSigner represents the CustomSigner type registration of the addon agent.
	// For this type, the addon agent can access the hub cluster through user-defined endpoints.
	RegistrationTypeCustomSigner RegistrationType = "CustomSigner"

	// HubPermissionsBindingSingleNamespace means that will only allow the addon agent to access the
	// resources in a single user defined namespace on the hub cluster.
	HubPermissionsBindingSingleNamespace HubPermissionsBindingType = "SingleNamespace"
	// HubPermissionsBindingCurrentCluster means that will only allow the addon agent to access the
	// resources in managed cluster namespace on the hub cluster.
	// It is a specific case of the SingleNamespace type.
	HubPermissionsBindingCurrentCluster HubPermissionsBindingType = "CurrentCluster"
)

// RegistrationSpec describes how to register an addon agent to the hub cluster.
// With the registration defined, The addon agent can access to kube apiserver with kube style API
// or other endpoints on hub cluster with client certificate authentication. During the addon
// registration process, a csr will be created for each Registration on the hub cluster. The
// CSR will be approved automatically, After the csr is approved on the hub cluster, the klusterlet
// agent will create a secret in the installNamespace for the addon agent.
// If the RegistrationType type is KubeClient, the secret name will be "{addon name}-hub-kubeconfig"
// whose content includes key/cert and kubeconfig. Otherwise, If the RegistrationType type is
// CustomSigner the secret name will be "{addon name}-{signer name}-client-cert" whose content
// includes key/cert.
type RegistrationSpec struct {
	// Type of the registration configuration, it supports:
	// - KubeClient: the addon agent can access the hub kube apiserver with kube style API.
	//   the signer name should be "kubernetes.io/kube-apiserver-client". When this type is
	//   used, the KubeClientRegistrationConfig can be used to define the permission of the
	//   addon agent to access the hub cluster
	// - CustomSigner: the addon agent can access the hub cluster through user-defined endpoints.
	//   When this type is used, the CustomSignerRegistrationConfig can be used to define how
	//   to issue the client certificate for the addon agent.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum:=KubeClient;CustomSigner
	Type RegistrationType `json:"type"`

	// KubeClient holds the configuration of the KubeClient type registration
	// +optional
	KubeClient *KubeClientRegistrationConfig `json:"kubeClient,omitempty"`

	// CustomSigner holds the configuration of the CustomSigner type registration
	// required when the Type is CustomSigner
	CustomSigner *CustomSignerRegistrationConfig `json:"customSigner,omitempty"`
}

type KubeClientRegistrationConfig struct {
	// HubPermissions represent the permission configurations of the addon agent to access the hub cluster
	// +optional
	HubPermissions []HubPermissionConfig `json:"hubPermissions,omitempty"`
}

// HubPermissionConfig configures the permission of the addon agent to access the hub cluster.
// Will create a RoleBinding in the same namespace as the managedClusterAddon to bind the user
// provided ClusterRole/Role to the "system:open-cluster-management:cluster:<cluster-name>:addon:<addon-name>"
// Group.
type HubPermissionConfig struct {
	// Type of the permissions setting. It defines how to bind the roleRef on the hub cluster. It can be:
	// - CurrentCluster: Bind the roleRef to the namespace with the same name as the managedCluster.
	// - SingleNamespace: Bind the roleRef to the namespace specified by SingleNamespaceBindingConfig.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum:=CurrentCluster;SingleNamespace
	Type HubPermissionsBindingType `json:"type"`

	// RoleRef is an reference to the permission resource. it could be a role or a cluster role,
	// the user must make sure it exist on the hub cluster.
	// +kubebuilder:validation:Required
	RoleRef rbacv1.RoleRef `json:"roleRef"`

	// SingleNamespace contains the configuration of SingleNamespace type binding.
	// It is required when the type is SingleNamespace
	SingleNamespace *SingleNamespaceBindingConfig `json:"singleNamespace,omitempty"`
}

type SingleNamespaceBindingConfig struct {
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`
}

type CustomSignerRegistrationConfig struct {
	// signerName is the name of signer that addon agent will use to create csr.
	// +required
	// +kubebuilder:validation:MaxLength=571
	// +kubebuilder:validation:MinLength=5
	SignerName string `json:"signerName"`

	// Subject is the user subject of the addon agent to be registered to the hub.
	// If it is not set, the addon agent will have the default subject
	// "subject": {
	//   "user": "system:open-cluster-management:cluster:{clusterName}:addon:{addonName}:agent:{agentName}",
	//   "groups: ["system:open-cluster-management:cluster:{clusterName}:addon:{addonName}",
	//             "system:open-cluster-management:addon:{addonName}", "system:authenticated"]
	// }
	Subject *Subject `json:"subject,omitempty"`

	// SigningCA represents the reference of the secret on the hub cluster to sign the CSR
	// the secret namespace must be "open-cluster-management-hub", and the secret type must be "kubernetes.io/tls"
	// +kubebuilder:validation:Required
	SigningCA SigningCARef `json:"signingCA"`
}

// SigningCARef is the reference to the signing CA secret which type must be "kubernetes.io/tls" and
// the namespace must be "open-cluster-management-hub"
type SigningCARef struct {
	// Name of the signing CA secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// AddOnTemplateList is a collection of addon templates.
type AddOnTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of addon templates.
	Items []AddOnTemplate `json:"items"`
}
