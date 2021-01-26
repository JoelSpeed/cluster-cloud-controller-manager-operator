package platform

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

type PlatformOwner interface {
	Init(*runtime.Scheme) bool
	GetOwner(ctx context.Context, с client.Client, key client.ObjectKey) (metav1.Object, []client.Object, error)
	Object() client.Object
	Mapper() handler.MapFunc
}
