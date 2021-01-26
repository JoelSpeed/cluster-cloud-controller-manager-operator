package cloud

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type OwnedResources []client.Object

func OwnedResourcesGroup() OwnedResources {
	resourceDistinct := OwnedResources{}
	resourceUnion := []OwnedResources{
		GetAWSResources(),
	}

	set := map[schema.GroupVersionKind]struct{}{}
	for _, platformGroup := range resourceUnion {
		for _, resource := range platformGroup {
			objType := resource.GetObjectKind().GroupVersionKind()
			klog.Info(objType)
			if _, ok := set[objType]; !ok {
				set[objType] = struct{}{}
				resourceDistinct = append(resourceDistinct, resource)
			}
		}
	}

	return resourceDistinct
}
