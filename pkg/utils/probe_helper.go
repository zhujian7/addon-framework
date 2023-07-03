package utils

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/agent"
)

// DeploymentProber is to check the addon status based on status
// of the agent deployment status
type DeploymentProber struct {
	deployments []types.NamespacedName
}

func NewDeploymentProber(deployments ...types.NamespacedName) *agent.HealthProber {
	probeFields := []agent.ProbeField{}
	for _, deploy := range deployments {
		probeFields = append(probeFields, agent.ProbeField{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Group:     "apps",
				Resource:  "deployments",
				Name:      deploy.Name,
				Namespace: deploy.Namespace,
			},
			ProbeRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.WellKnownStatusType,
				},
			},
		})
	}
	return &agent.HealthProber{
		Type: agent.HealthProberTypeWork,
		WorkProber: &agent.WorkHealthProber{
			ProbeFields: probeFields,
			HealthCheck: HealthCheck,
		},
	}
}

func (d *DeploymentProber) ProbeFields() []agent.ProbeField {
	probeFields := []agent.ProbeField{}
	for _, deploy := range d.deployments {
		probeFields = append(probeFields, agent.ProbeField{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Group:     "apps",
				Resource:  "deployments",
				Name:      deploy.Name,
				Namespace: deploy.Namespace,
			},
			ProbeRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.WellKnownStatusType,
				},
			},
		})
	}
	return probeFields
}

func HealthCheck(identifier workapiv1.ResourceIdentifier, result workapiv1.StatusFeedbackResult) error {
	if len(result.Values) == 0 {
		return fmt.Errorf("no values are probed for deployment %s/%s", identifier.Namespace, identifier.Name)
	}
	for _, value := range result.Values {
		if value.Name != "ReadyReplicas" {
			continue
		}

		if *value.Value.Integer >= 1 {
			return nil
		}

		return fmt.Errorf("readyReplica is %d for deployment %s/%s", *value.Value.Integer, identifier.Namespace, identifier.Name)
	}
	return fmt.Errorf("readyReplica is not probed")
}

func FilterDeployments(objects []runtime.Object) []*appsv1.Deployment {
	deployments := []*appsv1.Deployment{}
	for _, obj := range objects {
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			deployments = append(deployments, deployment)
		}
	}
	return deployments
}

func DeploymentWellKnowManifestConfig(deployment *appsv1.Deployment) workapiv1.ManifestConfigOption {
	return workapiv1.ManifestConfigOption{
		ResourceIdentifier: workapiv1.ResourceIdentifier{
			Group:     "apps",
			Resource:  "deployments",
			Name:      deployment.Name,
			Namespace: deployment.Namespace,
		},
		FeedbackRules: []workapiv1.FeedbackRule{
			{
				Type: workapiv1.WellKnownStatusType,
			},
		},
	}
}
