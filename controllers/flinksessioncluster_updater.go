/*
Copyright 2019 Google LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

// Updater which updates the status of a cluster based on the status of its
// components.

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	flinkoperatorv1alpha1 "github.com/googlecloudplatform/flink-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type _ClusterStatusUpdater struct {
	k8sClient     client.Client
	context       context.Context
	log           logr.Logger
	observedState _ObservedClusterState
}

// Compares the current status recorded in the cluster's status field and the
// new status derived from the status of the components, updates the cluster
// status if it is changed.
func (updater *_ClusterStatusUpdater) updateClusterStatusIfChanged() error {
	if updater.observedState.cluster == nil {
		updater.log.Info("The cluster has been deleted, no status to update")
		return nil
	}

	// Current status recorded in the cluster's status field.
	var currentStatus = flinkoperatorv1alpha1.FlinkSessionClusterStatus{}
	updater.observedState.cluster.Status.DeepCopyInto(&currentStatus)
	currentStatus.LastUpdateTime = ""

	// New status derived from the cluster's components.
	var newStatus = updater.deriveClusterStatus()

	// Compare
	var changed = updater.isStatusChanged(currentStatus, newStatus)

	// Update
	if changed {
		updater.log.Info(
			"Updating status",
			"current",
			updater.observedState.cluster.Status,
			"new", newStatus)
		newStatus.LastUpdateTime = time.Now().Format(time.RFC3339)
		return updater.updateClusterStatus(newStatus)
	} else {
		updater.log.Info("No status change")
	}
	return nil
}

func (updater *_ClusterStatusUpdater) deriveClusterStatus() flinkoperatorv1alpha1.FlinkSessionClusterStatus {
	var status = flinkoperatorv1alpha1.FlinkSessionClusterStatus{}
	var readyComponents = 0

	// JobManager deployment.
	var observedJmDeployment = updater.observedState.jmDeployment
	if observedJmDeployment != nil {
		status.Components.JobManagerDeployment.Name =
			observedJmDeployment.ObjectMeta.Name
		if observedJmDeployment.Status.AvailableReplicas <
			observedJmDeployment.Status.Replicas ||
			observedJmDeployment.Status.ReadyReplicas <
				observedJmDeployment.Status.Replicas {
			status.Components.JobManagerDeployment.State =
				flinkoperatorv1alpha1.ClusterComponentState.NotReady
		} else {
			status.Components.JobManagerDeployment.State =
				flinkoperatorv1alpha1.ClusterComponentState.Ready
			readyComponents++
		}
	}

	// JobManager service.
	var observedJmService = updater.observedState.jmService
	if observedJmService != nil {
		status.Components.JobManagerService.Name =
			observedJmService.ObjectMeta.Name
		status.Components.JobManagerService.State =
			flinkoperatorv1alpha1.ClusterComponentState.Ready
		readyComponents++
	}

	// TaskManager deployment.
	var observedTmDeployment = updater.observedState.tmDeployment
	if observedTmDeployment != nil {
		status.Components.TaskManagerDeployment.Name =
			observedTmDeployment.ObjectMeta.Name
		if observedTmDeployment.Status.AvailableReplicas <
			observedTmDeployment.Status.Replicas ||
			observedTmDeployment.Status.ReadyReplicas <
				observedTmDeployment.Status.Replicas {
			status.Components.TaskManagerDeployment.State =
				flinkoperatorv1alpha1.ClusterComponentState.NotReady
		} else {
			status.Components.TaskManagerDeployment.State =
				flinkoperatorv1alpha1.ClusterComponentState.Ready
			readyComponents++
		}
	}

	// (Optional) Job.
	var observedJob = updater.observedState.job
	if observedJob != nil {
		status.Components.Job = new(flinkoperatorv1alpha1.JobStatus)
		status.Components.Job.Name = observedJob.ObjectMeta.Name
		if observedJob.Status.Active > 0 {
			status.Components.Job.State = flinkoperatorv1alpha1.JobState.Running
		} else if observedJob.Status.Failed > 0 {
			status.Components.Job.State = flinkoperatorv1alpha1.JobState.Failed
		} else if observedJob.Status.Succeeded > 0 {
			status.Components.Job.State = flinkoperatorv1alpha1.JobState.Succeeded
		} else {
			status.Components.Job.State = flinkoperatorv1alpha1.JobState.Unknown
		}
	}

	if readyComponents < 3 {
		status.State = flinkoperatorv1alpha1.ClusterState.Reconciling
	} else {
		status.State = flinkoperatorv1alpha1.ClusterState.Running
	}

	return status
}

func (updater *_ClusterStatusUpdater) isStatusChanged(
	currentStatus flinkoperatorv1alpha1.FlinkSessionClusterStatus,
	newStatus flinkoperatorv1alpha1.FlinkSessionClusterStatus) bool {
	var changed = false
	if newStatus.State != currentStatus.State {
		changed = true
		updater.log.Info(
			"Cluster state changed",
			"current",
			currentStatus.State,
			"new",
			newStatus.State)
	}
	if newStatus.Components.JobManagerDeployment !=
		currentStatus.Components.JobManagerDeployment {
		updater.log.Info(
			"JobManager deployment status changed",
			"current", currentStatus.Components.JobManagerDeployment,
			"new",
			newStatus.Components.JobManagerDeployment)
		changed = true
	}
	if newStatus.Components.JobManagerService !=
		currentStatus.Components.JobManagerService {
		updater.log.Info(
			"JobManager service status changed",
			"current",
			currentStatus.Components.JobManagerService,
			"new", newStatus.Components.JobManagerService)
		changed = true
	}
	if newStatus.Components.TaskManagerDeployment !=
		currentStatus.Components.TaskManagerDeployment {
		updater.log.Info(
			"TaskManager deployment status changed",
			"current",
			currentStatus.Components.TaskManagerDeployment,
			"new",
			newStatus.Components.TaskManagerDeployment)
		changed = true
	}
	if currentStatus.Components.Job == nil {
		if newStatus.Components.Job != nil {
			updater.log.Info(
				"Job status changed",
				"current",
				"nil",
				"new",
				*newStatus.Components.Job)
			changed = true
		}
	} else {
		if *newStatus.Components.Job != *currentStatus.Components.Job {
			updater.log.Info(
				"Job status changed",
				"current",
				*currentStatus.Components.Job,
				"new",
				*newStatus.Components.Job)
			changed = true
		}
	}
	return changed
}

func (updater *_ClusterStatusUpdater) updateClusterStatus(
	status flinkoperatorv1alpha1.FlinkSessionClusterStatus) error {
	var flinkSessionCluster = flinkoperatorv1alpha1.FlinkSessionCluster{}
	updater.observedState.cluster.DeepCopyInto(&flinkSessionCluster)
	flinkSessionCluster.Status = status
	return updater.k8sClient.Update(updater.context, &flinkSessionCluster)
}