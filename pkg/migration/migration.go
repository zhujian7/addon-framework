package migration

import (
	"context"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	migrationv1alpha1client "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/typed/migration/v1alpha1"
)

const (
	migrationJitterFactor           = 0.25
	defaultMigrationDurationSeconds = 300
)

type migrationController struct {
	migrationDurationSeconds int32
	apiExtensionClient       apiextensionsclient.Interface

	migrateFromGVRs []schema.GroupVersionResource
	trigger         string

	crdStatusUpdateConditionFunc func() error
	crdStatusRemovalGVRs         []schema.GroupVersionResource
}

func NewCRDMigrationController(apiExtensionClient apiextensionsclient.Interface) *migrationController {
	return &migrationController{
		migrationDurationSeconds: defaultMigrationDurationSeconds,
		apiExtensionClient:       apiExtensionClient,
	}
}

// Start starts a cron task to reconcile the migration
func (c *migrationController) Start(ctx context.Context) {
	wait.JitterUntilWithContext(ctx, c.reconcile,
		time.Duration(c.migrationDurationSeconds)*time.Second, migrationJitterFactor, true)
}

// WithStorageVersionMigrationTrigger sets the GVRs that need to be triggered for migration
// With this set, the controller will create StorageVersionMigration CRs with label
// "open-cluster-management.io/created-by": "$triggerBy" for the given GVRs to trigger the migration
func (c *migrationController) WithStorageVersionMigrationTrigger(
	migrationClient migrationv1alpha1client.MigrationV1alpha1Interface,
	gvrs []schema.GroupVersionResource,
	trigger string,
) {
	c.migrateFromGVRs = gvrs
	c.trigger = trigger
}

// WithCRDStatusStoredVersionsRemoval sets the GVRs that need to be updated to remove the storedVersions in CRD status
// the crdStatusUpdateConditionFunc is used to check if it is safe to remove the storedVersions, the controller will
// check the condition periodically and remove the storedVersions if the condition is met
func (c *migrationController) WithCRDStatusStoredVersionsRemoval(
	crdStatusUpdateConditionFunc func() error,
	crdStatusRemovalGVRs []schema.GroupVersionResource,
) {
	c.crdStatusRemovalGVRs = crdStatusRemovalGVRs
	c.crdStatusUpdateConditionFunc = crdStatusUpdateConditionFunc
}

type DefaultMigrationCRDStatusUpdateConditionChecker struct {
	migrationClient migrationv1alpha1client.MigrationV1alpha1Interface
	gvrs            []schema.GroupVersionResource
}

func NewDefaultMigrationCRDStatusUpdateConditionChecker(
	migrationClient migrationv1alpha1client.MigrationV1alpha1Interface,
	gvrs []schema.GroupVersionResource,
) *DefaultMigrationCRDStatusUpdateConditionChecker {
	return &DefaultMigrationCRDStatusUpdateConditionChecker{
		migrationClient: migrationClient,
		gvrs:            gvrs,
	}
}

// CRDStatusUpdateConditionMigrationCRProcessed checks if the migration CRs with label
// "open-cluster-management.io/created-by" are processed successfully
func (c *DefaultMigrationCRDStatusUpdateConditionChecker) CRDStatusUpdateConditionMigrationCRProcessed() error {
	// TODO: implement the function
	return nil
}

func (c *migrationController) WithMigrationDurationSeconds(seconds int32) {
	c.migrationDurationSeconds = seconds
}

func (c *migrationController) reconcile(ctx context.Context) {
}
