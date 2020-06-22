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

package cluster

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"time"

	mapset "github.com/deckarep/golang-set"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	platformversionedclient "tkestack.io/tke/api/client/clientset/versioned/typed/platform/v1"
	platformv1informer "tkestack.io/tke/api/client/informers/externalversions/platform/v1"
	platformv1lister "tkestack.io/tke/api/client/listers/platform/v1"
	platformv1 "tkestack.io/tke/api/platform/v1"
	controllerutil "tkestack.io/tke/pkg/controller"
	"tkestack.io/tke/pkg/platform/controller/cluster/deletion"
	clusterprovider "tkestack.io/tke/pkg/platform/provider/cluster"
	typesv1 "tkestack.io/tke/pkg/platform/types/v1"
	"tkestack.io/tke/pkg/platform/util"
	"tkestack.io/tke/pkg/util/log"
	"tkestack.io/tke/pkg/util/metrics"
	"tkestack.io/tke/pkg/util/strategicpatch"
)

const (
	conditionTypeHealthCheck = "HealthCheck"
	failedHealthCheckReason  = "FailedHealthCheck"

	healthCheckInterval = 5 * time.Minute
)

// Controller is responsible for performing actions dependent upon a cluster phase.
type Controller struct {
	queue        workqueue.RateLimitingInterface
	lister       platformv1lister.ClusterLister
	listerSynced cache.InformerSynced

	log            log.Logger
	platformClient platformversionedclient.PlatformV1Interface
	healthCache    mapset.Set
	deleter        deletion.ClusterDeleterInterface
}

// NewController creates a new Controller object.
func NewController(
	platformClient platformversionedclient.PlatformV1Interface,
	clusterInformer platformv1informer.ClusterInformer,
	resyncPeriod time.Duration,
	finalizerToken platformv1.FinalizerName) *Controller {
	c := &Controller{
		queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "cluster"),

		log:            log.WithName("cluster-controller"),
		platformClient: platformClient,
		healthCache:    mapset.NewSet(),
		deleter: deletion.NewClusterDeleter(platformClient.Clusters(),
			platformClient,
			finalizerToken,
			true),
	}

	if platformClient != nil && platformClient.RESTClient().GetRateLimiter() != nil {
		_ = metrics.RegisterMetricAndTrackRateLimiterUsage("cluster_controller", platformClient.RESTClient().GetRateLimiter())
	}

	clusterInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.addCluster,
			UpdateFunc: c.updateCluster,
		},
		resyncPeriod,
	)
	c.lister = clusterInformer.Lister()
	c.listerSynced = clusterInformer.Informer().HasSynced

	return c
}

func (c *Controller) addCluster(obj interface{}) {
	cluster := obj.(*platformv1.Cluster)
	c.log.Info("Adding cluster", "clusterName", cluster.Name)
	c.enqueue(cluster)
}

func (c *Controller) updateCluster(old, obj interface{}) {
	oldCluster := old.(*platformv1.Cluster)
	cluster := obj.(*platformv1.Cluster)
	if !c.needsUpdate(oldCluster, cluster) {
		return
	}
	c.log.Info("Updating cluster", "clusterName", cluster.Name)
	c.enqueue(cluster)
}

func (c *Controller) enqueue(obj *platformv1.Cluster) {
	key, err := controllerutil.KeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %+v: %v", obj, err))
		return
	}
	c.queue.Add(key)
}

func (c *Controller) needsUpdate(old *platformv1.Cluster, new *platformv1.Cluster) bool {
	if !reflect.DeepEqual(old.Spec, new.Spec) {
		return true
	}

	if !reflect.DeepEqual(old.Status, new.Status) {
		return true
	}

	return false
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	log.Info("Starting cluster controller")
	defer log.Info("Shutting down cluster controller")

	if err := clusterprovider.Setup(); err != nil {
		return err
	}

	if ok := cache.WaitForCacheSync(stopCh, c.listerSynced); !ok {
		return fmt.Errorf("failed to wait for cluster caches to sync")
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh

	if err := clusterprovider.Teardown(); err != nil {
		return err
	}

	return nil
}

// worker processes the queue of persistent event objects.
// Each cluster can be in the queue at most once.
// The system ensures that no two workers can process
// the same namespace at the same time.
func (c *Controller) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncCluster(key.(string))
	if err == nil {
		c.queue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("error processing cluster %v (will retry): %v", key, err))
	c.queue.AddRateLimited(key)
	return true
}

// syncCluster will sync the Cluster with the given key if it has had
// its expectations fulfilled, meaning it did not expect to see any more of its
// namespaces created or deleted. This function is not meant to be invoked
// concurrently with the same key.
func (c *Controller) syncCluster(key string) error {
	logger := c.log.WithValues("cluster", key)

	startTime := time.Now()
	defer func() {
		logger.Info("Finished syncing cluster", "processTime", time.Since(startTime).String())
	}()

	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	cluster, err := c.lister.Get(name)
	if apierrors.IsNotFound(err) {
		logger.Info("cluster has been deleted")
	}
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to retrieve cluster %v from store: %v", key, err))
		return err
	}

	return c.reconcile(context.Background(), key, cluster)
}

func (c *Controller) reconcile(ctx context.Context, key string, cluster *platformv1.Cluster) error {
	logger := c.log.WithValues("cluster", cluster.Name)

	if err := c.ensureSyncOldClusterCredential(context.Background(), cluster); err != nil {
		return fmt.Errorf("sync old ClusterCredential error: %w", err)
	}

	var err error
	switch cluster.Status.Phase {
	case platformv1.ClusterInitializing:
		err = c.onCreate(ctx, cluster)
	case platformv1.ClusterRunning, platformv1.ClusterFailed:
		err = c.onUpdate(ctx, cluster)
	case platformv1.ClusterTerminating:
		logger.Info("Cluster has been terminated. Attempting to cleanup resources")
		err = c.deleter.Delete(context.Background(), key)
		if err == nil {
			logger.Info("Machine has been successfully deleted")
		}
	default:
		logger.Info("unknown cluster phase", "status.phase", cluster.Status.Phase)
	}

	return err
}

func (c *Controller) onCreate(ctx context.Context, cluster *platformv1.Cluster) error {
	provider, err := clusterprovider.GetProvider(cluster.Spec.Type)
	if err != nil {
		return err
	}
	if err := c.ensureClusterCredential(ctx, cluster); err != nil {
		return fmt.Errorf("ensureClusterCredential error: %w", err)
	}
	clusterWrapper, err := typesv1.GetCluster(ctx, c.platformClient, cluster)
	if err != nil {
		return err
	}

	// If any error happens, return error for retry.
	for clusterWrapper.Status.Phase == platformv1.ClusterInitializing {
		err = provider.OnCreate(ctx, clusterWrapper)
		_, err = c.platformClient.ClusterCredentials().Update(ctx, clusterWrapper.ClusterCredential, metav1.UpdateOptions{})
		_, err = c.platformClient.Clusters().Update(ctx, clusterWrapper.Cluster, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) onUpdate(ctx context.Context, cluster *platformv1.Cluster) error {
	provider, err := clusterprovider.GetProvider(cluster.Spec.Type)
	if err != nil {
		return err
	}

	clusterWrapper, err := typesv1.GetCluster(ctx, c.platformClient, cluster)
	if err != nil {
		return err
	}

	// If any error happens, return error for retry.
	err = provider.OnUpdate(ctx, clusterWrapper)
	_, err = c.platformClient.ClusterCredentials().Update(ctx, clusterWrapper.ClusterCredential, metav1.UpdateOptions{})
	_, err = c.platformClient.Clusters().Update(ctx, clusterWrapper.Cluster, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}

// ensureSyncOldClusterCredential using for sync old cluster without ClusterCredentialRef, will remove in next release.
func (c *Controller) ensureSyncOldClusterCredential(ctx context.Context, cluster *platformv1.Cluster) error {
	if cluster.Spec.ClusterCredentialRef != nil {
		return nil
	}

	fieldSelector := fields.OneTermEqualSelector("clusterName", cluster.Name).String()
	clusterCredentials, err := c.platformClient.ClusterCredentials().List(ctx, metav1.ListOptions{FieldSelector: fieldSelector})
	if err != nil {
		return err
	}
	if len(clusterCredentials.Items) == 0 {
		// Deprecated: will remove in next release
		if cluster.Spec.Type == "Imported" {
			return errors.New("waiting create ClusterCredential")
		}
		return nil
	}
	credential := &clusterCredentials.Items[0]
	cluster.Spec.ClusterCredentialRef = &corev1.LocalObjectReference{Name: credential.Name}
	_, err = c.platformClient.Clusters().Update(ctx, cluster, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) ensureClusterCredential(ctx context.Context, cluster *platformv1.Cluster) error {
	if cluster.Spec.ClusterCredentialRef == nil {
		// Deprecated: will remove in next release
		if cluster.Spec.Type == "Imported" { // don't precreate ClusterCredential for Imported cluster
			return nil
		}

		credential := &platformv1.ClusterCredential{
			TenantID:    cluster.Spec.TenantID,
			ClusterName: cluster.Name,
		}
		credential, err := c.platformClient.ClusterCredentials().Create(ctx, credential, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		cluster.Spec.ClusterCredentialRef = &corev1.LocalObjectReference{Name: credential.Name}
		_, err = c.platformClient.Clusters().Update(ctx, cluster, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	} else {
		credential, err := c.platformClient.ClusterCredentials().Get(ctx, cluster.Spec.ClusterCredentialRef.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if credential.ClusterName != cluster.Name {
			credential.ClusterName = cluster.Name
			_, err = c.platformClient.ClusterCredentials().Update(ctx, credential, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Controller) ensureStartHealthCheck(ctx context.Context, key string) {
	if c.healthCache.Contains(key) {
		return
	}
	logger := c.log.WithName("health-check").WithValues("cluster", key)
	logger.Info("Start health check loop")
	time.Sleep(time.Duration(rand.Intn(100)) * time.Microsecond)
	go wait.PollImmediateInfinite(healthCheckInterval, c.watchHealth(ctx, key))
	c.healthCache.Add(key)
}

// watchHealth check cluster health when phase in Running or Failed.
// Avoid affecting state machine operation.
func (c *Controller) watchHealth(ctx context.Context, key string) func() (bool, error) {
	return func() (bool, error) {
		logger := c.log.WithName("health-check").WithValues("cluster", key)

		cluster, err := c.lister.Get(key)
		if err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("Stop health check because cluster has been deleted")
				c.healthCache.Remove(key)
				return true, nil
			}
			return false, nil
		}

		if !(cluster.Status.Phase == platformv1.ClusterRunning || cluster.Status.Phase == platformv1.ClusterFailed) {
			return false, nil
		}

		err = c.checkHealth(ctx, cluster)
		if err != nil {
			logger.Error(err, "Check health error")
		}

		return false, nil
	}
}

func (c *Controller) checkHealth(ctx context.Context, cluster *platformv1.Cluster) error {
	oldCluster := cluster.DeepCopy()

	healthCheckCondition := platformv1.ClusterCondition{
		Type:   conditionTypeHealthCheck,
		Status: platformv1.ConditionFalse,
	}
	client, err := util.BuildExternalClientSet(ctx, cluster, c.platformClient)
	if err != nil {
		cluster.Status.Phase = platformv1.ClusterFailed

		healthCheckCondition.Reason = failedHealthCheckReason
		healthCheckCondition.Message = err.Error()
	} else {
		version, err := client.Discovery().ServerVersion()
		if err != nil {
			cluster.Status.Phase = platformv1.ClusterFailed

			healthCheckCondition.Reason = failedHealthCheckReason
			healthCheckCondition.Message = err.Error()
		} else {
			cluster.Status.Phase = platformv1.ClusterRunning
			cluster.Status.Version = version.String()

			healthCheckCondition.Status = platformv1.ConditionTrue
		}
	}

	cluster.SetCondition(healthCheckCondition)

	patchBytes, err := strategicpatch.GetPatchBytes(oldCluster, cluster)
	if err != nil {
		return fmt.Errorf("GetPatchBytes error: %w", err)
	}
	_, err = c.platformClient.Clusters().Patch(ctx, cluster.Name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("update cluster health status error: %w", err)
	}

	return nil
}
