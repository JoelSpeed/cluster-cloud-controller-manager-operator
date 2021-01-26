package controllers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var defaultResyncTime = 10 * time.Hour

type CacheOptions struct {
	Config *rest.Config
	Mapper meta.RESTMapper
	Scheme *runtime.Scheme
	Resync *time.Duration
}

type NamespacedCache interface {
	Watch(ctx context.Context, obj client.Object) error
	EventStream() <-chan event.GenericEvent
}

func NewNamespacedCache(opts CacheOptions) (NamespacedCache, error) {
	if opts.Config == nil {
		return nil, errors.New("Config is required")
	}

	// Use the default Kubernetes Scheme if unset
	if opts.Scheme == nil {
		opts.Scheme = scheme.Scheme
	}

	// Construct a new Mapper if unset
	if opts.Mapper == nil {
		var err error
		opts.Mapper, err = apiutil.NewDiscoveryRESTMapper(opts.Config)
		if err != nil {
			return nil, fmt.Errorf("could not create RESTMapper from config: %v", err)
		}
	}

	// Default the resync period to 10 hours if unset
	if opts.Resync == nil {
		opts.Resync = &defaultResyncTime
	}

	return &namespacedCache{
		caches:           make(map[string]cache.Cache),
		config:           opts.Config,
		mapper:           opts.Mapper,
		scheme:           opts.Scheme,
		resync:           opts.Resync,
		eventChan:        make(chan event.GenericEvent),
		watchedResources: make(map[string]map[string]struct{}),
	}, nil
}

type namespacedCache struct {
	caches           map[string]cache.Cache
	config           *rest.Config
	mapper           meta.RESTMapper
	scheme           *runtime.Scheme
	resync           *time.Duration
	eventChan        chan event.GenericEvent
	watchedResources map[string]map[string]struct{}
}

func (n *namespacedCache) EventStream() <-chan event.GenericEvent {
	return n.eventChan
}

func (n *namespacedCache) Watch(ctx context.Context, obj client.Object) error {
	// Check that namespace scoped objects have their namespace set
	if err := n.ensureNamespace(obj); err != nil {
		return err
	}

	namespacedWatches, ok := n.watchedResources[obj.GetNamespace()]
	if !ok {
		// No watch set up for this namespace yet
		return n.watch(ctx, obj)
	}

	key, err := n.watchKey(obj)
	if err != nil {
		return err
	}

	if _, ok := namespacedWatches[key]; !ok {
		// watch not set up for this object yet
		return n.watch(ctx, obj)
	}

	return nil
}

func (n *namespacedCache) watch(ctx context.Context, obj client.Object) error {
	informer, err := n.getInformer(ctx, obj)
	if err != nil {
		return nil
	}

	// Get the key before we set up the event to ensure we can mark the key in the watchedResources map
	key, err := n.watchKey(obj)
	if err != nil {
		return err
	}

	// Add an event handler that only allows events through for the correct object name
	// Since the informer is namespace bound, this should limit the events from this event handler to a single resource.
	informer.AddEventHandler(&eventToChannelHandler{
		name:       obj.GetName(),
		eventsChan: n.eventChan,
	})

	namespacedWatches, ok := n.watchedResources[obj.GetNamespace()]
	if !ok {
		namespacedWatches = make(map[string]struct{})
		n.watchedResources[obj.GetNamespace()] = namespacedWatches
	}
	namespacedWatches[key] = struct{}{}

	return nil
}

// getInformer gets a namespace limited informer for the object kind given.
// All non-namespaced objects will share a cluster wide cache.
// This cache should never be used for namespace scoped objects.
func (n *namespacedCache) getInformer(ctx context.Context, obj client.Object) (cache.Informer, error) {
	if err := n.ensureNamespace(obj); err != nil {
		return nil, err
	}

	c, ok := n.caches[obj.GetNamespace()]
	if ok {
		return c.GetInformer(ctx, obj)
	}

	c, err := cache.New(n.config, cache.Options{
		Scheme:    n.scheme,
		Mapper:    n.mapper,
		Resync:    n.resync,
		Namespace: obj.GetNamespace(),
	})
	if err != nil {
		return nil, err
	}
	n.caches[obj.GetNamespace()] = c
	go c.Start(ctx)

	return c.GetInformer(ctx, obj)
}

func (n *namespacedCache) isNamespaced(obj client.Object) (bool, error) {
	gvk, err := apiutil.GVKForObject(obj, n.scheme)
	if err != nil {
		return false, err
	}
	mapping, err := n.mapper.RESTMapping(gvk.GroupKind())
	if err != nil {
		return false, err
	}
	return mapping.Scope.Name() == meta.RESTScopeNameNamespace, nil
}

func (n *namespacedCache) ensureNamespace(obj client.Object) error {
	namespaced, err := n.isNamespaced(obj)
	if err != nil {
		return err
	}
	if namespaced && obj.GetNamespace() == "" {
		return errors.New("namespaced objects must set their namespace")
	}
	return nil
}

func (n *namespacedCache) watchKey(obj client.Object) (string, error) {
	gvk, err := apiutil.GVKForObject(obj, n.scheme)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s", gvk.GroupKind().String(), obj.GetName()), nil
}

type eventToChannelHandler struct {
	eventsChan chan event.GenericEvent
	name       string
}

func (e *eventToChannelHandler) OnAdd(obj interface{}) {
	e.queueEventForObject(obj)
}

func (e *eventToChannelHandler) OnUpdate(oldobj, obj interface{}) {
	e.queueEventForObject(obj)
}

func (e *eventToChannelHandler) OnDelete(obj interface{}) {
	e.queueEventForObject(obj)
}

// queueEventForObject sends the event onto the channel
func (e *eventToChannelHandler) queueEventForObject(o interface{}) {
	if o == nil {
		// Can't do anything here
		return
	}
	obj, ok := o.(client.Object)
	if !ok {
		return
	}
	if obj.GetName() != e.name {
		// Not the right object, skip
	}

	// Send an event to the events channel
	e.eventsChan <- event.GenericEvent{
		Object: obj,
	}
}
