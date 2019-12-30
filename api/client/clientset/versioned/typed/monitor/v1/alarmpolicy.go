/*
 * Tencent is pleased to support the open source community by making TKEStack
 * available.
 *
 * Copyright (C) 2012-2019 Tencent. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use
 * this file except in compliance with the License. You may obtain a copy of the
 * License at
 *
 * https://opensource.org/licenses/Apache-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
 * WARRANTIES OF ANY KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations under the License.
 */

// Code generated by client-gen. DO NOT EDIT.

package v1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
	scheme "tkestack.io/tke/api/client/clientset/versioned/scheme"
	v1 "tkestack.io/tke/api/monitor/v1"
)

// AlarmPoliciesGetter has a method to return a AlarmPolicyInterface.
// A group's client should implement this interface.
type AlarmPoliciesGetter interface {
	AlarmPolicies() AlarmPolicyInterface
}

// AlarmPolicyInterface has methods to work with AlarmPolicy resources.
type AlarmPolicyInterface interface {
	Create(*v1.AlarmPolicy) (*v1.AlarmPolicy, error)
	Update(*v1.AlarmPolicy) (*v1.AlarmPolicy, error)
	UpdateStatus(*v1.AlarmPolicy) (*v1.AlarmPolicy, error)
	Delete(name string, options *metav1.DeleteOptions) error
	Get(name string, options metav1.GetOptions) (*v1.AlarmPolicy, error)
	List(opts metav1.ListOptions) (*v1.AlarmPolicyList, error)
	Watch(opts metav1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.AlarmPolicy, err error)
	AlarmPolicyExpansion
}

// alarmPolicies implements AlarmPolicyInterface
type alarmPolicies struct {
	client rest.Interface
}

// newAlarmPolicies returns a AlarmPolicies
func newAlarmPolicies(c *MonitorV1Client) *alarmPolicies {
	return &alarmPolicies{
		client: c.RESTClient(),
	}
}

// Get takes name of the alarmPolicy, and returns the corresponding alarmPolicy object, and an error if there is any.
func (c *alarmPolicies) Get(name string, options metav1.GetOptions) (result *v1.AlarmPolicy, err error) {
	result = &v1.AlarmPolicy{}
	err = c.client.Get().
		Resource("alarmpolicies").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of AlarmPolicies that match those selectors.
func (c *alarmPolicies) List(opts metav1.ListOptions) (result *v1.AlarmPolicyList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1.AlarmPolicyList{}
	err = c.client.Get().
		Resource("alarmpolicies").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested alarmPolicies.
func (c *alarmPolicies) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Resource("alarmpolicies").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch()
}

// Create takes the representation of a alarmPolicy and creates it.  Returns the server's representation of the alarmPolicy, and an error, if there is any.
func (c *alarmPolicies) Create(alarmPolicy *v1.AlarmPolicy) (result *v1.AlarmPolicy, err error) {
	result = &v1.AlarmPolicy{}
	err = c.client.Post().
		Resource("alarmpolicies").
		Body(alarmPolicy).
		Do().
		Into(result)
	return
}

// Update takes the representation of a alarmPolicy and updates it. Returns the server's representation of the alarmPolicy, and an error, if there is any.
func (c *alarmPolicies) Update(alarmPolicy *v1.AlarmPolicy) (result *v1.AlarmPolicy, err error) {
	result = &v1.AlarmPolicy{}
	err = c.client.Put().
		Resource("alarmpolicies").
		Name(alarmPolicy.Name).
		Body(alarmPolicy).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *alarmPolicies) UpdateStatus(alarmPolicy *v1.AlarmPolicy) (result *v1.AlarmPolicy, err error) {
	result = &v1.AlarmPolicy{}
	err = c.client.Put().
		Resource("alarmpolicies").
		Name(alarmPolicy.Name).
		SubResource("status").
		Body(alarmPolicy).
		Do().
		Into(result)
	return
}

// Delete takes name of the alarmPolicy and deletes it. Returns an error if one occurs.
func (c *alarmPolicies) Delete(name string, options *metav1.DeleteOptions) error {
	return c.client.Delete().
		Resource("alarmpolicies").
		Name(name).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched alarmPolicy.
func (c *alarmPolicies) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.AlarmPolicy, err error) {
	result = &v1.AlarmPolicy{}
	err = c.client.Patch(pt).
		Resource("alarmpolicies").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}