package constants

import "fmt"

const (
	// AddonLabel is the label for addon
	AddonLabel = "open-cluster-management.io/addon-name"
	// AddonNamespaceLabel is the label for addon namespace
	AddonNamespaceLabel = "open-cluster-management.io/addon-namespace"

	// ClusterLabel is the label for cluster
	ClusterLabel = "open-cluster-management.io/cluster-name"

	// PreDeleteHookLabel is the label for a hook object
	PreDeleteHookLabel = "open-cluster-management.io/addon-pre-delete"

	// PreDeleteHookFinalizer is the finalizer for an addon which has deployed hook objects
	PreDeleteHookFinalizer = "cluster.open-cluster-management.io/addon-pre-delete"

	// AddonManifestApplied is a condition type representing whether the manifest of an addon
	// is applied correctly.
	AddonManifestApplied = "ManifestApplied"

	// AddonManifestAppliedReasonWorkApplyFailed is the reason of condition AddonManifestApplied indicating
	// the failuer of apply manifestwork of the manifests
	AddonManifestAppliedReasonWorkApplyFailed = "ManifestWorkApplyFailed"

	// AddonManifestAppliedReasonManifestsApplied is the reason of condition AddonManifestApplied indicating
	// the manifests is applied on the managedcluster.
	AddonManifestAppliedReasonManifestsApplied = "AddonManifestApplied"

	// AddonManifestAppliedReasonManifestsApplyFailed is the reason of condition AddonManifestApplied indicating
	// the failure to apply manifests on the managedcluster
	AddonManifestAppliedReasonManifestsApplyFailed = "AddonManifestAppliedFailed"

	// AddonHookManifestCompleted is a condition type representing whether the addon hook is completed.
	AddonHookManifestCompleted = "HookManifestCompleted"

	// // HostingClusterManifestLabel is the annotation for indicating the manifest should be deployed on the
	// // hosting cluster
	// HostingClusterManifestLabel = "addon.open-cluster-management.io/hosting-cluster-manifest"

	InstallModeBuiltinValueKey = "InstallMode"
	InstallModeHosted          = "Hosted"
	InstallModeDefault         = "Default"

	// HostingClusterNameAnnotation is the annotation for indicating the hosting cluster name
	HostingClusterNameAnnotation = "addon.open-cluster-management.io/hosting-cluster-name"

	// HostedManifestLocationLabel is the label for indicating where the manifest should be deployed in Hosted mode
	HostedManifestLocationLabel = "addon.open-cluster-management.io/hosted-manifest-location"

	// HostedManifestLocationManaged indicates the manifest will be deployed on the managed cluster in Hosted mode, it
	// is the default value of a manifest in Hosted mode
	HostedManifestLocationManaged = "managed"
	// HostedManifestLocationHosting indicates the manifest will be deployed on the hosting cluster in Hosted mode
	HostedManifestLocationHosting = "hosting"
	// HostedManifestLocationNone indicates the manifest will not be deployed in Hosted mode
	HostedManifestLocationNone = "none"

	// HostingManifestFinalizer is the finalizer for an addon which has deployed manifests on the external
	// hosting cluster in Hosted mode
	HostingManifestFinalizer = "cluster.open-cluster-management.io/hosting-manifests-cleanup"

	// AddonHostingManifestApplied is a condition type representing whether the manifest of an addon
	// is applied on the hosting cluster correctly.
	AddonHostingManifestApplied = "HostingManifestApplied"

	// HostingClusterValid is a condition type representing whether the hosting cluster is valid in Hosted mode
	HostingClusterValidity = "HostingClusterValidity"

	// HostingClusterValidityReasonValid is the reason of condition HostingClusterValidity indicating the hosting
	// cluster is valid
	HostingClusterValidityReasonValid = "HostingClusterValid"

	// HostingClusterValidityReasonInvalid is the reason of condition HostingClusterValidity indicating the hosting
	// cluster is invalid
	HostingClusterValidityReasonInvalid = "HostingClusterInvalid"
)

// DeployWorkName return the name of work for the addon
func DeployWorkName(addonName string) string {
	return fmt.Sprintf("addon-%s-deploy", addonName)
}

// DeployHostingWorkName return the name of manifest work on hosting cluster for the addon
func DeployHostingWorkName(addonNamespace, addonName string) string {
	return fmt.Sprintf("%s-hosting-%s", DeployWorkName(addonName), addonNamespace)
}

// GetHostedModeInfo returns addon installation mode and hosting cluster name.
func GetHostedModeInfo(annotations map[string]string) (string, string) {
	hostingClusterName, ok := annotations[HostingClusterNameAnnotation]
	if !ok {
		return InstallModeDefault, ""
	}

	return InstallModeHosted, hostingClusterName
}

// GetHostedManifestLocation returns the location of the manifest in Hosted mode, if it is invalid will return error
func GetHostedManifestLocation(labels map[string]string) (string, bool, error) {
	manifestLocation, ok := labels[HostedManifestLocationLabel]
	if !ok {
		return "", false, nil
	}

	switch manifestLocation {
	case HostedManifestLocationManaged, HostedManifestLocationHosting, HostedManifestLocationNone:
		return manifestLocation, true, nil
	default:
		return "", true, fmt.Errorf("not supported manifest location: %s", manifestLocation)
	}
}
