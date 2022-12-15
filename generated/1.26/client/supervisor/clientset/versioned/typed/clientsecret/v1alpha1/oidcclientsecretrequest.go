// Copyright 2020-2022 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"

	v1alpha1 "go.pinniped.dev/generated/1.26/apis/supervisor/clientsecret/v1alpha1"
	scheme "go.pinniped.dev/generated/1.26/client/supervisor/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rest "k8s.io/client-go/rest"
)

// OIDCClientSecretRequestsGetter has a method to return a OIDCClientSecretRequestInterface.
// A group's client should implement this interface.
type OIDCClientSecretRequestsGetter interface {
	OIDCClientSecretRequests(namespace string) OIDCClientSecretRequestInterface
}

// OIDCClientSecretRequestInterface has methods to work with OIDCClientSecretRequest resources.
type OIDCClientSecretRequestInterface interface {
	Create(ctx context.Context, oIDCClientSecretRequest *v1alpha1.OIDCClientSecretRequest, opts v1.CreateOptions) (*v1alpha1.OIDCClientSecretRequest, error)
	OIDCClientSecretRequestExpansion
}

// oIDCClientSecretRequests implements OIDCClientSecretRequestInterface
type oIDCClientSecretRequests struct {
	client rest.Interface
	ns     string
}

// newOIDCClientSecretRequests returns a OIDCClientSecretRequests
func newOIDCClientSecretRequests(c *ClientsecretV1alpha1Client, namespace string) *oIDCClientSecretRequests {
	return &oIDCClientSecretRequests{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Create takes the representation of a oIDCClientSecretRequest and creates it.  Returns the server's representation of the oIDCClientSecretRequest, and an error, if there is any.
func (c *oIDCClientSecretRequests) Create(ctx context.Context, oIDCClientSecretRequest *v1alpha1.OIDCClientSecretRequest, opts v1.CreateOptions) (result *v1alpha1.OIDCClientSecretRequest, err error) {
	result = &v1alpha1.OIDCClientSecretRequest{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("oidcclientsecretrequests").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(oIDCClientSecretRequest).
		Do(ctx).
		Into(result)
	return
}
