package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

const (
	eventuallyTimeout  = 300 // seconds
	eventuallyInterval = 1   // seconds
	addonName          = "helloworld"
)

var _ = ginkgo.Describe("install/uninstall helloworld addons", func() {
	ginkgo.BeforeEach(func() {
		gomega.Eventually(func() error {
			_, err := hubClusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			_, err = hubKubeClient.CoreV1().Namespaces().Get(context.Background(), managedClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Install/uninstall addon and make sure it is available", func() {
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), addonName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					_, cerr := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(),
						&addonapiv1alpha1.ManagedClusterAddOn{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: managedClusterName,
								Name:      addonName,
							},
							Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
								InstallNamespace: "default",
							},
						}, metav1.CreateOptions{})
					if cerr != nil {
						return cerr
					}
				}
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "ManifestApplied") {
				return fmt.Errorf("addon should be applied to spoke, but get condition %v", addon.Status.Conditions)
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("addon should be available on spoke, but get condition %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Ensure helloworld addo is functioning.
		configmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("config-%s", rand.String(6)),
				Namespace: managedClusterName,
			},
			Data: map[string]string{
				"key1": rand.String(6),
				"key2": rand.String(6),
			},
		}

		_, err := hubKubeClient.CoreV1().ConfigMaps(managedClusterName).Create(context.Background(), configmap, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			copyiedConfig, err := hubKubeClient.CoreV1().ConfigMaps("default").Get(context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !apiequality.Semantic.DeepEqual(copyiedConfig.Data, configmap.Data) {
				return fmt.Errorf("expected configmap is not correct, %v", copyiedConfig.Data)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// remove addon
		err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(context.Background(), addonName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})
})
