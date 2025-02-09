package addonprogressing

import (
	"context"
	"encoding/json"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/utils"
	"open-cluster-management.io/api/addon/v1alpha1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakework "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func newClusterManagementOwner(name string) metav1.OwnerReference {
	clusterManagementAddon := addontesting.NewClusterManagementAddon(name, "testcrd", "testcr").Build()
	return *metav1.NewControllerRef(clusterManagementAddon, addonapiv1alpha1.GroupVersion.WithKind("ClusterManagementAddOn"))
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                   string
		syncKey                string
		managedClusteraddon    []runtime.Object
		clusterManagementAddon []runtime.Object
		work                   []runtime.Object
		validateAddonActions   func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                   "no clustermanagementaddon",
			syncKey:                "test/test",
			clusterManagementAddon: []runtime.Object{},
			managedClusteraddon:    []runtime.Object{},
			work:                   []runtime.Object{},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:                   "no managedClusteraddon",
			syncKey:                "test/test",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work:                   []runtime.Object{},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:    "no work applied condition",
			syncKey: "test/test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work:                   []runtime.Object{},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:    "update managedclusteraddon to installing when no work",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work:                   []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonInstalling && configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
			},
		},
		{
			name:    "update managedclusteraddon to installing when work config spec not match",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "",
						},
					},
				}
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"cluster1",
					addontesting.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					addontesting.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey: "test",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hash\"}",
				})
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
					{
						Type:   workapiv1.WorkAvailable,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonInstalling && configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 0 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
			},
		},
		{
			name:    "update managedclusteraddon to installing when work is not ready",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "",
						},
					},
				}
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"cluster1",
					addontesting.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					addontesting.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey: "test",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hashnew\"}",
				})
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionFalse,
					},
					{
						Type:   workapiv1.WorkAvailable,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonInstalling && configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 0 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
			},
		},
		{
			name:    "update managedclusteraddon to uprading when work config spec not match",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hash",
						},
					},
				}
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"cluster1",
					addontesting.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					addontesting.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey: "test",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hash\"}",
				})
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
					{
						Type:   workapiv1.WorkAvailable,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonUpgrading && configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 0 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
			},
		},
		{
			name:    "update managedclusteraddon to uprading when work is not ready",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hash",
						},
					},
				}
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"cluster1",
					addontesting.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					addontesting.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey: "test",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hashnew\"}",
				})
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
					{
						Type:   workapiv1.WorkAvailable,
						Status: metav1.ConditionFalse,
					},
				}
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonUpgrading && configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 0 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
			},
		},
		{
			name:    "update managedclusteraddon to install succeed",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "",
						},
					},
				}
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"cluster1",
					addontesting.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					addontesting.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey: "test",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hashnew\"}",
				})
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
					{
						Type:   workapiv1.WorkAvailable,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonInstallSucceed && configCond.Status == metav1.ConditionFalse) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 1 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
				if addOn.Status.ConfigReferences[0].LastAppliedConfig.SpecHash != addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash {
					t.Errorf("LastAppliedConfig object is not correct: %v", addOn.Status.ConfigReferences[0].LastAppliedConfig.SpecHash)
				}
			},
		},
		{
			name:    "update managedclusteraddon to upgrade succeed",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hash",
						},
					},
				}
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"cluster1",
					addontesting.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					addontesting.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey: "test",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hashnew\"}",
				})
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
					{
						Type:   workapiv1.WorkAvailable,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonUpgradeSucceed && configCond.Status == metav1.ConditionFalse) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 1 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
				if addOn.Status.ConfigReferences[0].LastAppliedConfig.SpecHash != addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash {
					t.Errorf("LastAppliedConfig object is not correct: %v", addOn.Status.ConfigReferences[0].LastAppliedConfig.SpecHash)
				}
			},
		},
		{
			name:    "update managedclusteraddon to configuration unsupported...",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "config1.test",
								Resource: "config1",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "override",
							},
						},
					}
					addon.Status.SupportedConfigs = []addonapiv1alpha1.ConfigGroupResource{
						{
							Group:    "configs.test",
							Resource: "testconfigs",
						},
					}
					meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "manifests of addon are applied successfully",
					})
					return addon
				}(),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work:                   []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}

				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonConfigurationUnsupported && configCond.Status == metav1.ConditionFalse) {
					t.Errorf("Condition Progressing is incorrect")
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.managedClusteraddon...)
			fakeWorkClient := fakework.NewSimpleClientset()

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			workInformers := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)

			for _, obj := range c.managedClusteraddon {
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.clusterManagementAddon {
				if err := addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.work {
				if err := workInformers.Work().V1().ManifestWorks().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := addonProgressingController{
				addonClient:                  fakeAddonClient,
				managedClusterAddonLister:    addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				clusterManagementAddonLister: addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
				workLister:                   workInformers.Work().V1().ManifestWorks().Lister(),
				addonFilterFunc:              utils.ManagedBySelf(map[string]agent.AgentAddon{"test": nil}),
			}

			syncContext := addontesting.NewFakeSyncContext(t)
			err := controller.sync(context.TODO(), syncContext, c.syncKey)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}
