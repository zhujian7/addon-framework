package addonfactory

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/valyala/fasttemplate"
	appsv1 "k8s.io/api/apps/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
)

var AddOnTemplateGVR = schema.GroupVersionResource{
	Group:    "addon.open-cluster-management.io",
	Version:  "v1alpha1",
	Resource: "addontemplates",
}

// templateBuiltinValues includes the built-in values for crd template agentAddon.
// the values for template config should begin with an uppercase letter, so we need
// to convert it to Values by JsonStructToValues.
// the built-in values can not be overridden by getValuesFuncs
type templateCRDBuiltinValues struct {
	ClusterName           string `json:"CLUSTER_NAME,omitempty"`
	AddonInstallNamespace string `json:"INSTALL_NAMESPACE,omitempty"`
	InstallMode           string `json:"INSTALL_MODE,omitempty"`
}

// templateDefaultValues includes the default values for crd template agentAddon.
// the values for template config should begin with an uppercase letter, so we need
// to convert it to Values by JsonStructToValues.
// the default values can be overridden by getValuesFuncs
type templateCRDDefaultValues struct {
	HubKubeConfigPath     string `json:"HUB_KUBECONFIG,omitempty"`
	ManagedKubeConfigPath string `json:"MANAGED_KUBECONFIG,omitempty"`
}

type CRDTemplateAgentAddon struct {
	getValuesFuncs     []GetValuesFunc
	agentAddonOptions  agent.AgentAddonOptions
	trimCRDDescription bool

	hubKubeClient kubernetes.Interface
	addonClient   addonv1alpha1client.Interface
	addonName     string
	templateSpec  addonapiv1alpha1.AddOnTemplateSpec
}

// NewCRDTemplateAgentAddon creates a CRDTemplateAgentAddon instance
func NewCRDTemplateAgentAddon(
	hubKubeClient kubernetes.Interface,
	addonClient addonv1alpha1client.Interface,
	templateSpec addonapiv1alpha1.AddOnTemplateSpec,
	getValuesFuncs ...GetValuesFunc,
) *CRDTemplateAgentAddon {
	a := &CRDTemplateAgentAddon{
		getValuesFuncs:     getValuesFuncs,
		trimCRDDescription: true,

		hubKubeClient: hubKubeClient,
		addonClient:   addonClient,
		addonName:     templateSpec.AddonName,
		templateSpec:  templateSpec,
	}

	a.agentAddonOptions = agent.AgentAddonOptions{
		AddonName:           templateSpec.AddonName,
		Registration:        a.newRegistrationOption(utilrand.String(5)),
		InstallStrategy:     nil,
		HealthProber:        nil,
		SupportedConfigGVRs: []schema.GroupVersionResource{},
	}
	return a
}

// addonTemplateConfigRef return the first addon template config
func (a *CRDTemplateAgentAddon) addonTemplateConfigRef(
	configReferences []addonapiv1alpha1.ConfigReference) (bool, addonapiv1alpha1.ConfigReference) {
	for _, config := range configReferences {
		if config.Group == AddOnTemplateGVR.Group && config.Resource == AddOnTemplateGVR.Resource {
			return true, config
		}
	}
	return false, addonapiv1alpha1.ConfigReference{}
}

func (a *CRDTemplateAgentAddon) Manifests(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {

	return a.renderObjects(cluster, addon)
}

func (a *CRDTemplateAgentAddon) renderObjects(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	var objects []runtime.Object
	presetValues, configValues, err := a.getValues(cluster, addon)
	if err != nil {
		return objects, err
	}
	klog.Infof("##### configValues: %v", configValues)
	var wg sync.WaitGroup
	wg.Add(1)
	var gerr error
	go func() {
		defer wg.Done()

		for _, manifest := range a.templateSpec.AgentManifests {

			t := fasttemplate.New(string(manifest.Raw), "{{", "}}")
			manifestStr := t.ExecuteString(configValues)
			klog.Infof(" ====== test, render result: %v", manifestStr)
			object := &unstructured.Unstructured{}
			if err := object.UnmarshalJSON([]byte(manifestStr)); err != nil {
				gerr = err
				return
			}
			objects = append(objects, object)
		}
	}()
	wg.Wait()
	if gerr != nil {
		return objects, gerr
	}

	for _, decorator := range []decorateObjects{a.injectEnvironments, a.injectVolumes} {
		objects, err = decorator(objects, presetValues)
		if err != nil {
			return objects, err
		}
	}
	return objects, nil
}

type decorateObjects func(objects []runtime.Object, values orderedValues) ([]runtime.Object, error)

func (a *CRDTemplateAgentAddon) injectEnvironments(
	objects []runtime.Object, values orderedValues,
) ([]runtime.Object, error) {

	envVars := make([]corev1.EnvVar, len(values))
	for index, value := range values {
		envVars[index] = corev1.EnvVar{
			Name:  value.name,
			Value: value.value,
		}
	}

	for i, obj := range objects {
		deployment, err := a.convertToDeployment(obj)
		if err != nil {
			continue
		}
		for j, _ := range deployment.Spec.Template.Spec.Containers {
			deployment.Spec.Template.Spec.Containers[j].Env = append(
				deployment.Spec.Template.Spec.Containers[j].Env,
				envVars...)
		}

		objects[i] = deployment
	}
	return objects, nil
}

func (a *CRDTemplateAgentAddon) injectVolumes(
	objects []runtime.Object, values orderedValues,
) ([]runtime.Object, error) {

	for index, obj := range objects {
		deployment, err := a.convertToDeployment(obj)
		if err != nil {
			continue
		}
		for j := range deployment.Spec.Template.Spec.Containers {
			deployment.Spec.Template.Spec.Containers[j].VolumeMounts = append(
				deployment.Spec.Template.Spec.Containers[j].VolumeMounts,
				corev1.VolumeMount{
					Name:      "hub-kubeconfig",
					MountPath: "/managed/hub-kubeconfig",
				})
		}

		deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "hub-kubeconfig",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: a.hubKubeconfigSecretName(),
				},
			},
		})

		// TODO: inject managed kubeconfig for hosted mode
		objects[index] = deployment
	}
	return objects, nil
}

func (a *CRDTemplateAgentAddon) convertToDeployment(obj runtime.Object) (*appsv1.Deployment, error) {
	if obj.GetObjectKind().GroupVersionKind().Group != "apps" ||
		obj.GetObjectKind().GroupVersionKind().Kind != "Deployment" {
		return nil, fmt.Errorf("not deployment object, %v", obj.GetObjectKind())
	}

	deployment := &appsv1.Deployment{}
	uobj, ok := obj.(*unstructured.Unstructured)
	if ok {
		err := runtime.DefaultUnstructuredConverter.
			FromUnstructured(uobj.Object, deployment)
		if err != nil {
			return nil, err
		}
		return deployment, nil
	}

	deployment, ok = obj.(*appsv1.Deployment)
	if ok {
		return deployment, nil
	}

	return nil, fmt.Errorf("not deployment object, %v", obj.GetObjectKind())
}

func (a *CRDTemplateAgentAddon) GetAgentAddonOptions() agent.AgentAddonOptions {
	return a.agentAddonOptions
}

type keyValuePair struct {
	name  string
	value string
}

type orderedValues []keyValuePair

func (a *CRDTemplateAgentAddon) getValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (orderedValues, map[string]interface{}, error) {
	presetValues := make([]keyValuePair, 0)
	overrideValues := map[string]interface{}{}

	defaultSortedKeys, defaultValues, err := a.getDefaultValues(cluster, addon)
	if err != nil {
		return presetValues, overrideValues, nil
	}
	overrideValues = MergeValues(overrideValues, defaultValues)

	for i := 0; i < len(a.getValuesFuncs); i++ {
		if a.getValuesFuncs[i] != nil {
			userValues, err := a.getValuesFuncs[i](cluster, addon)
			if err != nil {
				return nil, nil, err
			}
			overrideValues = MergeValues(overrideValues, userValues)
		}
	}
	builtinSortedKeys, builtinValues, err := a.getBuiltinValues(cluster, addon)
	if err != nil {
		return presetValues, overrideValues, nil
	}
	overrideValues = MergeValues(overrideValues, builtinValues)

	for k, v := range overrideValues {
		_, ok := v.(string)
		if !ok {
			return nil, nil, fmt.Errorf("only support string type for variables, invalid key %s", k)
		}
	}

	keys := append(defaultSortedKeys, builtinSortedKeys...)

	for _, key := range keys {
		presetValues = append(presetValues, keyValuePair{
			name:  key,
			value: overrideValues[key].(string),
		})
	}
	return presetValues, overrideValues, nil
}

func (a *CRDTemplateAgentAddon) getBuiltinValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]string, Values, error) {
	builtinValues := templateCRDBuiltinValues{}
	builtinValues.ClusterName = cluster.GetName()

	installNamespace := addon.Spec.InstallNamespace
	if len(installNamespace) == 0 {
		installNamespace = AddonDefaultInstallNamespace
	}
	builtinValues.AddonInstallNamespace = installNamespace
	builtinValues.InstallMode, _ = constants.GetHostedModeInfo(addon.GetAnnotations())

	value, err := JsonStructToValues(builtinValues)
	if err != nil {
		return nil, nil, err
	}
	return a.sortValueKeys(value), value, nil
}

func (a *CRDTemplateAgentAddon) getDefaultValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]string, Values, error) {
	defaultValues := templateCRDDefaultValues{}

	// TODO: hubKubeConfigSecret depends on the signer configuration in registration, and the registration is an array.
	if a.agentAddonOptions.Registration != nil {
		defaultValues.HubKubeConfigPath = a.hubKubeconfigPath()
	}

	if constants.IsHostedMode(addon.GetAnnotations()) {
		defaultValues.ManagedKubeConfigPath = a.managedKubeconfigPath()
	}

	value, err := JsonStructToValues(defaultValues)
	if err != nil {
		return nil, nil, err
	}
	return a.sortValueKeys(value), value, nil
}

func (a *CRDTemplateAgentAddon) sortValueKeys(value Values) []string {
	keys := make([]string, 0)
	for k, _ := range value {
		keys = append(keys, k)
	}

	sort.Strings(keys)
	return keys
}

func (a *CRDTemplateAgentAddon) hubKubeconfigPath() string {
	return "/managed/hub-kubeconfig/kubeconfig"
}

func (a *CRDTemplateAgentAddon) managedKubeconfigPath() string {
	return "/managed/kubeconfig/kubeconfig"
}

func (a *CRDTemplateAgentAddon) hubKubeconfigSecretName() string {
	return fmt.Sprintf("%s-hub-kubeconfig", a.agentAddonOptions.AddonName)
}

func (a *CRDTemplateAgentAddon) managedKubeconfigSecretName() string {
	return fmt.Sprintf("%s-managed-kubeconfig", a.agentAddonOptions.AddonName)
}

func (a *CRDTemplateAgentAddon) newRegistrationOption(
	agentName string) *agent.RegistrationOption {
	registrationOption := &agent.RegistrationOption{}
	registrationConfigFuncs := make([]func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig, 0)
	csrApprovers := make(map[string]agent.CSRApproveFunc, 0)
	csrSigners := make([]agent.CSRSignerFunc, 0)

	for _, registration := range a.templateSpec.Registration {
		switch registration.Type {
		case addonapiv1alpha1.RegistrationTypeKubeClient:
			registrationConfigFuncs = append(registrationConfigFuncs,
				agent.KubeClientSignerConfigurations(a.addonName, agentName))

			csrApprovers[certificatesv1.KubeAPIServerClientSignerName] = utils.KubeClientCSRApprover(agentName)

			registrationOption.PermissionConfig = utils.TemplateAddonHubPermission(
				a.hubKubeClient, registration.KubeClient)
		case addonapiv1alpha1.RegistrationTypeCustomSigner:
			registrationConfigFuncs = append(registrationConfigFuncs,
				agent.CustomSignerConfigurations(a.addonName, agentName, registration.CustomSigner))

			if registration.CustomSigner != nil {
				csrApprovers[registration.CustomSigner.SignerName] = utils.CustomerSignerCSRApprover(agentName)
			}

			csrSigners = append(csrSigners, utils.CustomSignerWithExpiry(
				a.hubKubeClient, registration.CustomSigner, 24*time.Hour))
		default:
			utilruntime.HandleError(fmt.Errorf("unsupported registration type %s", registration.Type))
		}

	}

	registrationOption.CSRConfigurations = utils.UnionSignerConfiguration(registrationConfigFuncs...)
	registrationOption.CSRApproveCheck = utils.UnionCSRApprover(csrApprovers)
	registrationOption.CSRSign = utils.UnionCSRSigner(csrSigners...)
	return registrationOption
}
