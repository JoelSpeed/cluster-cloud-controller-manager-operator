package platform

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openshift/cluster-cloud-controller-manager-operator/tmp/pkg/cloud"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

var _ PlatformOwner = &InfrastrucutreOwner{}

type InfrastrucutreOwner struct {
	objects []configv1.Infrastructure
}

func (o *InfrastrucutreOwner) Object() client.Object {
	return &configv1.Infrastructure{}
}

func (o *InfrastrucutreOwner) Init(scheme *runtime.Scheme) bool {
	c, err := client.New(ctrl.GetConfigOrDie(), client.Options{
		Scheme: scheme,
	})
	if err != nil {
		klog.Fatalf("Unable to open client: %v", err)
	}

	infraList := &configv1.InfrastructureList{}
	if err := c.List(context.TODO(), infraList); err != nil {
		klog.Errorf("Unable to retrive list of platform %T objects: %v", infraList, err)
		return false
	} else if len(infraList.Items) == 0 {
		return false
	}

	o.objects = infraList.Items
	return true
}

func (o *InfrastrucutreOwner) Mapper() handler.MapFunc {
	mapObjects := []reconcile.Request{}
	for _, infra := range o.objects {
		mapObjects = append(mapObjects, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&infra),
		})
	}
	return func(client.Object) []reconcile.Request { return mapObjects }
}

func (o *InfrastrucutreOwner) GetOwner(ctx context.Context, с client.Client, key client.ObjectKey) (metav1.Object, []client.Object, error) {
	infra := &configv1.Infrastructure{}
	err := с.Get(ctx, key, infra)
	if err != nil {
		klog.Errorf("Unable to retrive platform %T object: %v", infra, err)
		return nil, nil, err
	}

	return infra, getResources(infra.Status.Platform), nil
}

func getResources(platformType configv1.PlatformType) []client.Object {
	switch platformType {
	case configv1.AWSPlatformType:
		return cloud.GetAWSResources()
	default:
		klog.Warning("No recognized cloud provider platform found in infrastructure")
	}
	return nil
}
