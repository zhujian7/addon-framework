package addonprogressing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/agentdeploy"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
)

const (
	ProgressingDoing   string = "Doing"
	ProgressingSucceed string = "Succeed"
	ProgressingFailed  string = "Failed"
)

// addonProgressingController reconciles instances of managedclusteradd on the hub
// based to update the status progressing condition and last applied config
type addonProgressingController struct {
	addonClient                  addonv1alpha1client.Interface
	managedClusterAddonLister    addonlisterv1alpha1.ManagedClusterAddOnLister
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
	workLister                   worklister.ManifestWorkLister
	addonFilterFunc              factory.EventFilterFunc
}

func NewAddonProgressingController(
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	workInformers workinformers.ManifestWorkInformer,
	addonFilterFunc factory.EventFilterFunc,
) factory.Controller {
	c := &addonProgressingController{
		addonClient:                  addonClient,
		managedClusterAddonLister:    addonInformers.Lister(),
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
		workLister:                   workInformers.Lister(),
		addonFilterFunc:              addonFilterFunc,
	}

	return factory.New().WithInformersQueueKeysFunc(
		func(obj runtime.Object) []string {
			key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			return []string{key}
		},
		addonInformers.Informer(), clusterManagementAddonInformers.Informer()).
		// TODO: consider hosted manifestwork
		WithInformersQueueKeysFunc(
			func(obj runtime.Object) []string {
				accessor, _ := meta.Accessor(obj)
				return []string{fmt.Sprintf("%s/%s", accessor.GetNamespace(), accessor.GetLabels()[addonapiv1alpha1.AddonLabelKey])}
			},
			workInformers.Informer()).
		WithSync(c.sync).ToController("addon-progressing-controller")
}

func (c *addonProgressingController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	klog.V(4).Infof("Reconciling addon %q", key)

	namespace, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is invalid
		return nil
	}

	addon, err := c.managedClusterAddonLister.ManagedClusterAddOns(namespace).Get(addonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	clusterManagementAddon, err := c.clusterManagementAddonLister.Get(addonName)
	if errors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return err
	}

	if !c.addonFilterFunc(clusterManagementAddon) {
		return nil
	}

	// update progressing condition and last applied config
	return c.updateAddonProgressingAndLastApplied(ctx, addon.DeepCopy(), addon)
}

func (c *addonProgressingController) updateAddonProgressingAndLastApplied(ctx context.Context, newaddon, oldaddon *addonapiv1alpha1.ManagedClusterAddOn) error {
	// check config references
	if supported, config := isConfigurationSupported(newaddon); !supported {
		meta.SetStatusCondition(&newaddon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
			Status:  metav1.ConditionFalse,
			Reason:  addonapiv1alpha1.ProgressingReasonConfigurationUnsupported,
			Message: fmt.Sprintf("Configuration with gvr %s/%s is not supported for this addon", config.Group, config.Resource),
		})
		return c.patchAddOnProgressingAndLastApplied(ctx, newaddon, oldaddon)
	}

	// wait until addon has ManifestApplied condition
	if cond := meta.FindStatusCondition(newaddon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnManifestApplied); cond == nil {
		meta.SetStatusCondition(&newaddon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
			Status:  metav1.ConditionFalse,
			Reason:  "WaitingForManifestApplied",
			Message: "Waiting for ManagedClusterAddOn ManifestApplied condition",
		})
		return c.patchAddOnProgressingAndLastApplied(ctx, newaddon, oldaddon)
	}

	// set upgrade flag
	isUpgrade := false
	for _, configReference := range newaddon.Status.ConfigReferences {
		if configReference.LastAppliedConfig != nil && configReference.LastAppliedConfig.SpecHash != "" {
			isUpgrade = true
			break
		}
	}

	// get addon works
	// TODO: consider hosted manifestwork
	requirement, _ := labels.NewRequirement(addonapiv1alpha1.AddonLabelKey, selection.Equals, []string{newaddon.Name})
	selector := labels.NewSelector().Add(*requirement)
	addonWorks, err := c.workLister.ManifestWorks(newaddon.Namespace).List(selector)
	if err != nil {
		setAddOnProgressingAndLastApplied(isUpgrade, ProgressingFailed, err.Error(), newaddon)
		return c.patchAddOnProgressingAndLastApplied(ctx, newaddon, oldaddon)
	}

	if len(addonWorks) == 0 {
		setAddOnProgressingAndLastApplied(isUpgrade, ProgressingDoing, "no addon works", newaddon)
		return c.patchAddOnProgressingAndLastApplied(ctx, newaddon, oldaddon)
	}

	// check addon manifestworks
	for _, work := range addonWorks {
		// skip pre-delete manifestwork
		if strings.HasPrefix(work.Name, constants.PreDeleteHookWorkName(newaddon.Name)) {
			continue
		}

		// check if work configs matches addon configs
		if !workConfigsMatchesAddon(work, newaddon) {
			setAddOnProgressingAndLastApplied(isUpgrade, ProgressingDoing, "configs mismatch", newaddon)
			return c.patchAddOnProgressingAndLastApplied(ctx, newaddon, oldaddon)
		}

		// check if work is ready
		if !workIsReady(work) {
			setAddOnProgressingAndLastApplied(isUpgrade, ProgressingDoing, "work is not ready", newaddon)
			return c.patchAddOnProgressingAndLastApplied(ctx, newaddon, oldaddon)
		}
	}

	// set lastAppliedConfig when all the work matches addon and are ready.
	setAddOnProgressingAndLastApplied(isUpgrade, ProgressingSucceed, "", newaddon)
	return c.patchAddOnProgressingAndLastApplied(ctx, newaddon, oldaddon)
}

func (c *addonProgressingController) patchAddOnProgressingAndLastApplied(ctx context.Context, new, old *addonapiv1alpha1.ManagedClusterAddOn) error {
	if equality.Semantic.DeepEqual(new.Status, old.Status) {
		return nil
	}

	oldData, err := json.Marshal(&addonapiv1alpha1.ManagedClusterAddOn{
		Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
			ConfigReferences: old.Status.ConfigReferences,
			Conditions:       old.Status.Conditions,
		},
	})
	if err != nil {
		return err
	}

	newData, err := json.Marshal(&addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			UID:             new.UID,
			ResourceVersion: new.ResourceVersion,
		},
		Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
			ConfigReferences: new.Status.ConfigReferences,
			Conditions:       new.Status.Conditions,
		},
	})
	if err != nil {
		return err
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return fmt.Errorf("failed to create patch for addon %s: %w", new.Name, err)
	}

	klog.V(2).Infof("Patching addon %s/%s condition and last applied config with %s", new.Namespace, new.Name, string(patchBytes))
	addon, err := c.addonClient.AddonV1alpha1().ManagedClusterAddOns(new.Namespace).Patch(
		ctx, new.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	fmt.Printf("%v", addon)
	return err
}

func isConfigurationSupported(addon *addonapiv1alpha1.ManagedClusterAddOn) (bool, addonapiv1alpha1.ConfigGroupResource) {
	supportedConfigSet := map[addonapiv1alpha1.ConfigGroupResource]bool{}
	for _, config := range addon.Status.SupportedConfigs {
		supportedConfigSet[config] = true
	}

	for _, config := range addon.Spec.Configs {
		if _, ok := supportedConfigSet[config.ConfigGroupResource]; !ok {
			return false, config.ConfigGroupResource
		}
	}

	return true, addonapiv1alpha1.ConfigGroupResource{}
}

func workConfigsMatchesAddon(work *workapiv1.ManifestWork, addon *addonapiv1alpha1.ManagedClusterAddOn) bool {
	// get work spec hash
	if _, ok := work.Annotations[workapiv1.ManifestConfigSpecHashAnnotationKey]; !ok {
		return len(addon.Status.ConfigReferences) == 0
	}

	// parse work spec hash
	workSpecHashMap := make(map[string]string)
	if err := json.Unmarshal([]byte(work.Annotations[workapiv1.ManifestConfigSpecHashAnnotationKey]), &workSpecHashMap); err != nil {
		klog.Warningf("%v", err)
		return false
	}

	// check work spec hash, all the config should have spec hash
	for _, v := range workSpecHashMap {
		if v == "" {
			return false
		}
	}

	// check addon desired config
	for _, configReference := range addon.Status.ConfigReferences {
		if configReference.DesiredConfig == nil || configReference.DesiredConfig.SpecHash == "" {
			return false
		}
	}
	addonSpecHashMap := agentdeploy.ConfigsToMap(addon.Status.ConfigReferences)

	// compare work and addon configs
	return equality.Semantic.DeepEqual(workSpecHashMap, addonSpecHashMap)
}

// work is ready when
// 1) condition Available status is true.
// 2) condition Available observedGeneration equals to generation.
// 3) If it is a fresh install since one addon can have multiple ManifestWorks, the ManifestWork condition ManifestApplied must also be true.
func workIsReady(work *workapiv1.ManifestWork) bool {
	cond := meta.FindStatusCondition(work.Status.Conditions, workapiv1.WorkAvailable)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != work.Generation {
		return false
	}
	cond = meta.FindStatusCondition(work.Status.Conditions, workapiv1.WorkApplied)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != work.Generation {
		return false
	}

	return true
}

// set addon progressing condition and last applied
func setAddOnProgressingAndLastApplied(isUpgrade bool, status string, message string, addon *addonapiv1alpha1.ManagedClusterAddOn) {
	// always update progressing condition when there is no config
	// skip update progressing condition when last applied config already the same as desired
	skip := len(addon.Status.ConfigReferences) > 0
	for _, configReference := range addon.Status.ConfigReferences {
		if !equality.Semantic.DeepEqual(configReference.LastAppliedConfig, configReference.DesiredConfig) {
			skip = false
		}
	}
	if skip {
		return
	}

	condition := metav1.Condition{
		Type: addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
	}
	switch status {
	case ProgressingDoing:
		condition.Status = metav1.ConditionTrue
		if isUpgrade {
			condition.Reason = addonapiv1alpha1.ProgressingReasonUpgrading
			condition.Message = fmt.Sprintf("upgrading... %v", message)
		} else {
			condition.Reason = addonapiv1alpha1.ProgressingReasonInstalling
			condition.Message = fmt.Sprintf("installing... %v", message)
		}
	case ProgressingSucceed:
		condition.Status = metav1.ConditionFalse
		for i, configReference := range addon.Status.ConfigReferences {
			addon.Status.ConfigReferences[i].LastAppliedConfig = configReference.DesiredConfig.DeepCopy()
		}
		if isUpgrade {
			condition.Reason = addonapiv1alpha1.ProgressingReasonUpgradeSucceed
			condition.Message = "upgrade completed with no errors."
		} else {
			condition.Reason = addonapiv1alpha1.ProgressingReasonInstallSucceed
			condition.Message = "install completed with no errors."
		}
	case ProgressingFailed:
		condition.Status = metav1.ConditionFalse
		if isUpgrade {
			condition.Reason = addonapiv1alpha1.ProgressingReasonUpgradeFailed
			condition.Message = message
		} else {
			condition.Reason = addonapiv1alpha1.ProgressingReasonInstallFailed
			condition.Message = message
		}
	}
	meta.SetStatusCondition(&addon.Status.Conditions, condition)
}
