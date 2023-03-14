package utils

import (
	"context"
	"fmt"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	rbacclientv1 "k8s.io/client-go/kubernetes/typed/rbac/v1"
	"k8s.io/utils/pointer"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/addon-framework/pkg/agent"
)

const (
	RoleRefKindUser = "User"
)

// RBACPermissionBuilder builds a agent.PermissionConfigFunc that applies Kubernetes RBAC policies.
type RBACPermissionBuilder interface {
	// BindClusterRoleToUser is a shortcut that ensures a cluster role and binds to a hub user.
	BindClusterRoleToUser(clusterRole *rbacv1.ClusterRole, username string) RBACPermissionBuilder
	// BindClusterRoleToGroup is a shortcut that ensures a cluster role and binds to a hub user group.
	BindClusterRoleToGroup(clusterRole *rbacv1.ClusterRole, userGroup string) RBACPermissionBuilder
	// BindRoleToUser is a shortcut that ensures a role and binds to a hub user.
	BindRoleToUser(clusterRole *rbacv1.Role, username string) RBACPermissionBuilder
	// BindRoleToGroup is a shortcut that ensures a role binding and binds to a hub user.
	BindRoleToGroup(clusterRole *rbacv1.Role, userGroup string) RBACPermissionBuilder

	// WithStaticClusterRole ensures a cluster role to the hub cluster.
	WithStaticClusterRole(clusterRole *rbacv1.ClusterRole) RBACPermissionBuilder
	// WithStaticClusterRoleBinding ensures a cluster role binding to the hub cluster.
	WithStaticClusterRoleBinding(clusterRole *rbacv1.ClusterRoleBinding) RBACPermissionBuilder
	// WithStaticRole ensures a role to the hub cluster.
	WithStaticRole(clusterRole *rbacv1.Role) RBACPermissionBuilder
	// WithStaticRole ensures a role binding to the hub cluster.
	WithStaticRoleBinding(clusterRole *rbacv1.RoleBinding) RBACPermissionBuilder

	// Build wraps up the builder chain, and return a agent.PermissionConfigFunc.
	Build() agent.PermissionConfigFunc
}

var _ RBACPermissionBuilder = &permissionBuilder{}

type permissionBuilder struct {
	kubeClient kubernetes.Interface
	u          *unionPermissionBuilder
}

// NewRBACPermissionConfigBuilder instantiates a default RBACPermissionBuilder.
func NewRBACPermissionConfigBuilder(kubeClient kubernetes.Interface) RBACPermissionBuilder {
	return &permissionBuilder{
		u:          &unionPermissionBuilder{},
		kubeClient: kubeClient,
	}
}

func (p *permissionBuilder) BindClusterRoleToUser(clusterRole *rbacv1.ClusterRole, username string) RBACPermissionBuilder {
	return p.WithStaticClusterRole(clusterRole).
		WithStaticClusterRoleBinding(&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterRole.Name, // The same name as the cluster role
			},
			RoleRef: rbacv1.RoleRef{
				Kind: "ClusterRole",
				Name: clusterRole.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind: rbacv1.UserKind,
					Name: username,
				},
			},
		})
}

func (p *permissionBuilder) BindClusterRoleToGroup(clusterRole *rbacv1.ClusterRole, userGroup string) RBACPermissionBuilder {
	return p.WithStaticClusterRole(clusterRole).
		WithStaticClusterRoleBinding(&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterRole.Name, // The same name as the cluster role
			},
			RoleRef: rbacv1.RoleRef{
				Kind: "ClusterRole",
				Name: clusterRole.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind: rbacv1.GroupKind,
					Name: userGroup,
				},
			},
		})
}

func (p *permissionBuilder) BindRoleToUser(role *rbacv1.Role, username string) RBACPermissionBuilder {
	return p.WithStaticRole(role).
		WithStaticRoleBinding(&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: role.Name, // The same name as the cluster role
			},
			RoleRef: rbacv1.RoleRef{
				Kind: "Role",
				Name: role.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind: rbacv1.UserKind,
					Name: username,
				},
			},
		})
}

func (p *permissionBuilder) BindRoleToGroup(role *rbacv1.Role, userGroup string) RBACPermissionBuilder {
	return p.WithStaticRole(role).
		WithStaticRoleBinding(&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: role.Name, // The same name as the cluster role
			},
			RoleRef: rbacv1.RoleRef{
				Kind: "Role",
				Name: role.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind: rbacv1.GroupKind,
					Name: userGroup,
				},
			},
		})
}

func (p *permissionBuilder) WithStaticClusterRole(clusterRole *rbacv1.ClusterRole) RBACPermissionBuilder {
	p.u.fns = append(p.u.fns, func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
		_, _, err := ApplyClusterRole(context.TODO(), p.kubeClient.RbacV1(), clusterRole)
		return err
	})
	return p
}

func (p *permissionBuilder) WithStaticClusterRoleBinding(binding *rbacv1.ClusterRoleBinding) RBACPermissionBuilder {
	p.u.fns = append(p.u.fns, func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
		_, _, err := ApplyClusterRoleBinding(context.TODO(), p.kubeClient.RbacV1(), binding)
		return err
	})
	return p
}

func (p *permissionBuilder) WithStaticRole(role *rbacv1.Role) RBACPermissionBuilder {
	p.u.fns = append(p.u.fns, func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
		role.Namespace = cluster.Name
		ensureAddonOwnerReference(&role.ObjectMeta, addon)
		_, _, err := ApplyRole(context.TODO(), p.kubeClient.RbacV1(), role)
		return err
	})
	return p
}

func (p *permissionBuilder) WithStaticRoleBinding(binding *rbacv1.RoleBinding) RBACPermissionBuilder {
	p.u.fns = append(p.u.fns, func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
		binding.Namespace = cluster.Name
		ensureAddonOwnerReference(&binding.ObjectMeta, addon)
		_, _, err := ApplyRoleBinding(context.TODO(), p.kubeClient.RbacV1(), binding)
		return err
	})
	return p
}

func (p *permissionBuilder) Build() agent.PermissionConfigFunc {
	return p.u.build()
}

type unionPermissionBuilder struct {
	fns []agent.PermissionConfigFunc
}

func (b *unionPermissionBuilder) build() agent.PermissionConfigFunc {
	return func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
		for _, fn := range b.fns {
			if err := fn(cluster, addon); err != nil {
				return err
			}
		}
		return nil
	}
}

func ensureAddonOwnerReference(metadata *metav1.ObjectMeta, addon *addonapiv1alpha1.ManagedClusterAddOn) {
	metadata.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion:         addonapiv1alpha1.GroupVersion.String(),
			Kind:               "ManagedClusterAddOn",
			Name:               addon.Name,
			BlockOwnerDeletion: pointer.Bool(true),
			UID:                addon.UID,
		},
	}
}

// ApplyClusterRole merges objectmeta, requires rules, aggregation rules are not allowed for now.
func ApplyClusterRole(ctx context.Context, client rbacclientv1.ClusterRolesGetter, required *rbacv1.ClusterRole) (*rbacv1.ClusterRole, bool, error) {
	if required.AggregationRule != nil && len(required.AggregationRule.ClusterRoleSelectors) != 0 {
		return nil, false, fmt.Errorf("cannot create an aggregated cluster role")
	}

	existing, err := client.ClusterRoles().Get(ctx, required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.ClusterRoles().Create(
			ctx, requiredCopy, metav1.CreateOptions{})
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	existingCopy := existing.DeepCopy()
	contentSame := equality.Semantic.DeepEqual(existingCopy.Rules, required.Rules)
	if contentSame {
		return existingCopy, false, nil
	}

	existingCopy.Rules = required.Rules
	existingCopy.AggregationRule = nil

	actual, err := client.ClusterRoles().Update(ctx, existingCopy, metav1.UpdateOptions{})
	return actual, true, err
}

// ApplyClusterRoleBinding merges objectmeta, requires subjects and role refs
// TODO on non-matching roleref, delete and recreate
func ApplyClusterRoleBinding(ctx context.Context,
	client rbacclientv1.ClusterRoleBindingsGetter,
	required *rbacv1.ClusterRoleBinding) (*rbacv1.ClusterRoleBinding, bool, error) {
	existing, err := client.ClusterRoleBindings().Get(ctx, required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.ClusterRoleBindings().Create(
			ctx, requiredCopy, metav1.CreateOptions{})
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	existingCopy := existing.DeepCopy()
	requiredCopy := required.DeepCopy()

	// Enforce apiGroup fields in roleRefs
	existingCopy.RoleRef.APIGroup = rbacv1.GroupName
	for i := range existingCopy.Subjects {
		if existingCopy.Subjects[i].Kind == RoleRefKindUser {
			existingCopy.Subjects[i].APIGroup = rbacv1.GroupName
		}
	}

	requiredCopy.RoleRef.APIGroup = rbacv1.GroupName
	for i := range requiredCopy.Subjects {
		if requiredCopy.Subjects[i].Kind == RoleRefKindUser {
			requiredCopy.Subjects[i].APIGroup = rbacv1.GroupName
		}
	}

	subjectsAreSame := equality.Semantic.DeepEqual(existingCopy.Subjects, requiredCopy.Subjects)
	roleRefIsSame := equality.Semantic.DeepEqual(existingCopy.RoleRef, requiredCopy.RoleRef)

	if subjectsAreSame && roleRefIsSame {
		return existingCopy, false, nil
	}

	existingCopy.Subjects = requiredCopy.Subjects
	existingCopy.RoleRef = requiredCopy.RoleRef

	actual, err := client.ClusterRoleBindings().Update(ctx, existingCopy, metav1.UpdateOptions{})
	return actual, true, err
}

// ApplyRole merges objectmeta, requires rules
func ApplyRole(ctx context.Context, client rbacclientv1.RolesGetter, required *rbacv1.Role) (*rbacv1.Role, bool, error) {
	existing, err := client.Roles(required.Namespace).Get(ctx, required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.Roles(required.Namespace).Create(
			ctx, requiredCopy, metav1.CreateOptions{})
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	existingCopy := existing.DeepCopy()

	contentSame := equality.Semantic.DeepEqual(existingCopy.Rules, required.Rules)
	if contentSame {
		return existingCopy, false, nil
	}

	existingCopy.Rules = required.Rules

	actual, err := client.Roles(required.Namespace).Update(ctx, existingCopy, metav1.UpdateOptions{})
	return actual, true, err
}

// ApplyRoleBinding merges objectmeta, requires subjects and role refs
// TODO on non-matching roleref, delete and recreate
func ApplyRoleBinding(ctx context.Context, client rbacclientv1.RoleBindingsGetter, required *rbacv1.RoleBinding) (*rbacv1.RoleBinding, bool, error) {
	existing, err := client.RoleBindings(required.Namespace).Get(ctx, required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.RoleBindings(required.Namespace).Create(
			ctx, requiredCopy, metav1.CreateOptions{})
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	existingCopy := existing.DeepCopy()
	requiredCopy := required.DeepCopy()

	// Enforce apiGroup fields in roleRefs and subjects
	existingCopy.RoleRef.APIGroup = rbacv1.GroupName
	for i := range existingCopy.Subjects {
		if existingCopy.Subjects[i].Kind == RoleRefKindUser {
			existingCopy.Subjects[i].APIGroup = rbacv1.GroupName
		}
	}

	requiredCopy.RoleRef.APIGroup = rbacv1.GroupName
	for i := range requiredCopy.Subjects {
		if requiredCopy.Subjects[i].Kind == RoleRefKindUser {
			requiredCopy.Subjects[i].APIGroup = rbacv1.GroupName
		}
	}

	subjectsAreSame := equality.Semantic.DeepEqual(existingCopy.Subjects, requiredCopy.Subjects)
	roleRefIsSame := equality.Semantic.DeepEqual(existingCopy.RoleRef, requiredCopy.RoleRef)

	if subjectsAreSame && roleRefIsSame {
		return existingCopy, false, nil
	}

	existingCopy.Subjects = requiredCopy.Subjects
	existingCopy.RoleRef = requiredCopy.RoleRef

	actual, err := client.RoleBindings(requiredCopy.Namespace).Update(ctx, existingCopy, metav1.UpdateOptions{})
	return actual, true, err
}

// TemplateAddonHubPermission returns a func that can grant permission for addon agent
// that is deployed by addon template.
// the returned func will create a rolebinding to bind the clusterRole/role which is
// specified by the user, so the is required to make sure the existence of the
// clusterRole/role
func TemplateAddonHubPermission(hubKubeClient kubernetes.Interface,
	kcrc *addonapiv1alpha1.KubeClientRegistrationConfig) agent.PermissionConfigFunc {
	return func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
		if kcrc == nil {
			return nil
		}

		if kcrc.Permissions == nil {
			return nil
		}

		for _, pc := range kcrc.Permissions {
			switch pc.Type {
			case addonapiv1alpha1.HubPermissionsBindingCurrentCluster:
				owner := metav1.OwnerReference{
					APIVersion: addon.APIVersion,
					Kind:       addon.Kind,
					Name:       addon.Name,
					UID:        addon.UID,
				}
				err := createPermissionBinding(hubKubeClient,
					cluster.Name, addon.Name, cluster.Name, pc.RoleRef, owner)
				if err != nil {
					return err
				}
			case addonapiv1alpha1.HubPermissionsBindingSingleNamespace:
				if pc.SingleNamespace == nil {
					return fmt.Errorf("single namespace is nil")
				}
				owner := metav1.OwnerReference{
					APIVersion: cluster.APIVersion,
					Kind:       cluster.Kind,
					Name:       cluster.Name,
					UID:        cluster.UID,
				}
				err := createPermissionBinding(hubKubeClient,
					cluster.Name, addon.Name, pc.SingleNamespace.Namespace, pc.RoleRef, owner)
				if err != nil {
					return err
				}
			}
		}

		return nil
	}
}

func createPermissionBinding(hubKubeClient kubernetes.Interface,
	clusterName, addonName, namespace string, roleRef rbacv1.RoleRef, owner metav1.OwnerReference) error {
	// TODO: confirm the group
	groups := agent.DefaultGroups(clusterName, addonName)
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("open-cluster-management:%s:%s:agent",
				addonName, strings.ToLower(roleRef.Kind)),
			Namespace:       namespace,
			OwnerReferences: []metav1.OwnerReference{owner},
		},
		RoleRef: roleRef,
		Subjects: []rbacv1.Subject{
			{Kind: "Group", APIGroup: "rbac.authorization.k8s.io", Name: groups[0]},
		},
	}

	// TODO: check the existence of the role, cluster role
	_, err := hubKubeClient.RbacV1().RoleBindings(namespace).Get(context.TODO(), binding.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		_, createErr := hubKubeClient.RbacV1().RoleBindings(namespace).Create(context.TODO(), binding, metav1.CreateOptions{})
		if createErr != nil {
			return createErr
		}
	case err != nil:
		return err
	}
	return nil
}
