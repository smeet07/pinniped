// Copyright 2020-2021 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	time "time"

	idpv1alpha1 "go.pinniped.dev/generated/1.19/apis/supervisor/idp/v1alpha1"
	versioned "go.pinniped.dev/generated/1.19/client/supervisor/clientset/versioned"
	internalinterfaces "go.pinniped.dev/generated/1.19/client/supervisor/informers/externalversions/internalinterfaces"
	v1alpha1 "go.pinniped.dev/generated/1.19/client/supervisor/listers/idp/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// ActiveDirectoryIdentityProviderInformer provides access to a shared informer and lister for
// ActiveDirectoryIdentityProviders.
type ActiveDirectoryIdentityProviderInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.ActiveDirectoryIdentityProviderLister
}

type activeDirectoryIdentityProviderInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewActiveDirectoryIdentityProviderInformer constructs a new informer for ActiveDirectoryIdentityProvider type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewActiveDirectoryIdentityProviderInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredActiveDirectoryIdentityProviderInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredActiveDirectoryIdentityProviderInformer constructs a new informer for ActiveDirectoryIdentityProvider type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredActiveDirectoryIdentityProviderInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.IDPV1alpha1().ActiveDirectoryIdentityProviders(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.IDPV1alpha1().ActiveDirectoryIdentityProviders(namespace).Watch(context.TODO(), options)
			},
		},
		&idpv1alpha1.ActiveDirectoryIdentityProvider{},
		resyncPeriod,
		indexers,
	)
}

func (f *activeDirectoryIdentityProviderInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredActiveDirectoryIdentityProviderInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *activeDirectoryIdentityProviderInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&idpv1alpha1.ActiveDirectoryIdentityProvider{}, f.defaultInformer)
}

func (f *activeDirectoryIdentityProviderInformer) Lister() v1alpha1.ActiveDirectoryIdentityProviderLister {
	return v1alpha1.NewActiveDirectoryIdentityProviderLister(f.Informer().GetIndexer())
}
