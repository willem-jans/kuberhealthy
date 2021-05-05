// Copyright 2018 Comcast Cable Communications Management, LLC
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.package main

package main

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
)

// CheckWorker represents a worker for a specific pod and node.
type CheckWorker struct {
	Check *CheckInfo
}

// NewWorker returns a new worker with its given check task.
// func NewWorker(ctx context.Context, check CheckInfo) CheckWorker {
func NewWorker(check *CheckInfo) CheckWorker {
	return CheckWorker{
		Check: check,
	}
}

// NewWorker returns a new worker with its given check task.
// func NewWorker(ctx context.Context, check CheckInfo) CheckWorker {
func NewWorkerv2(node *v1.Node) CheckWorker {
	return CheckWorker{
		Check: &CheckInfo{
			Node: node,
		},
	}
}

// PerformCheck makes the worker perform its assigned check.
func (w CheckWorker) PerformCheck(ctx context.Context, resultChan chan *CheckInfo, wg *sync.WaitGroup) {

	defer wg.Done()

	result := w.Check

	// Apply
	log.Debugln("WORKER CALLING deploy pod to node Pod:", w.Check.Pod.Name, "Node:", w.Check.Node.Name)
	log.Infoln("Deploying pod", w.Check.Pod.Name, "to node", w.Check.Node.Name)
	pod, err := deployPodToNode(ctx, w.Check.Pod, w.Check.Node)
	if err != nil {
		log.Errorln("Worker failed to deploy pod", w.Check.Pod.Name, "to node", w.Check.Node.Name+":", err)
		result.Errors = append(result.Errors, err)
		resultChan <- result
		return
	}
	log.Infoln("Pod", w.Check.Pod.Name, "deployed to", pod.Spec.NodeName+".")

	// Watch
	log.Infoln("Watching pod", w.Check.Pod.Name, "to come online on node", w.Check.Node.Name)
	err = watchPodRunning(ctx, pod)
	if err != nil {
		log.Errorln("Failed to watch pod", pod.Name, "come online on node", w.Check.Node.Name+":", err)
		result.Errors = append(result.Errors, err)

		// Attempt to clean up if we fail to watch the pod
		deleteErr := deletePodFromNode(ctx, pod, w.Check.Node)
		result.Errors = append(result.Errors, deleteErr)
		resultChan <- result
		return
	}
	log.Infoln("Pod", w.Check.Pod.Name, "is online and running.")

	// Wait for the pod to run for some time
	time.Sleep(podTTLSeconds)

	// Delete
	log.Infoln("Deleting and watching pod", w.Check.Pod.Name, "to be removed from node", w.Check.Node.Name)
	err = deletePodFromNode(ctx, pod, w.Check.Node)
	if err != nil {
		log.Errorln("Failed to remove pod", pod.Name, "from node", w.Check.Node.Name+":", err)
		result.Errors = append(result.Errors, err)
		resultChan <- result
		return
	}
	log.Infoln("Pod", w.Check.Pod.Name, "deleted.")

	// Report
	resultChan <- result
}

// PerformCheck makes the worker perform its assigned check.
func (w CheckWorker) PerformCheckv2(ctx context.Context, resultChan chan *CheckInfo, wg *sync.WaitGroup) {

	log.Debugln("Worker beginning check on node", w.Check.Node.Name)

	errs := make([]error, 0)

	defer func() {
		w.Check.Errors = errs
		resultChan <- w.Check
		wg.Done()
	}()

	// Create the pod spec.
	podSpec := createPodSpec(w.Check.Node)

	// Apply
	// log.Debugln("WORKER CALLING deploy pod to node Pod:", w.Check.Pod.Name, "Node:", w.Check.Node.Name)
	log.Infoln("Deploying pod", podSpec.Name, "to node", podSpec.Spec.NodeName)
	// pod, err := deployPodToNode(ctx, w.Check.Pod, w.Check.Node)
	pod, err := deployPodToNodev2(ctx, podSpec)
	w.Check.Pod = pod
	if err != nil {
		log.Errorln("Worker failed to deploy pod", podSpec.Name, "to node", w.Check.Node.Name+":", err)
		errs = append(errs, err)
		// resultChan <- w.Check
		return
	}
	log.Infoln("Pod", w.Check.Pod.Name, "deployed to", w.Check.Pod.Spec.NodeName+".")

	// Watch
	log.Infoln("Watching pod", w.Check.Pod.Name, "to come online on node", w.Check.Node.Name)
	err = watchPodRunning(ctx, w.Check.Pod)
	if err != nil {
		log.Errorln("Failed to watch pod", pod.Name, "come online on node", w.Check.Node.Name+":", err)
		errs = append(errs, err)

		// Attempt to clean up if we fail to watch the pod
		deleteErr := deletePodFromNode(ctx, pod, w.Check.Node)
		errs = append(errs, deleteErr)
		// resultChan <- w.Check
		return
	}
	// log.Infoln("Pod", w.Check.Pod.Name, "is online and running.")
	// log.Debugln("Sleeping for", defaultWorkerInterlude, "seconds before removing pod.")
	// time.Sleep(time.Second * defaultWorkerInterlude)

	// Wait for the pod to run for some time
	time.Sleep(podTTLSeconds)

	// Delete
	log.Infoln("Deleting and watching pod", w.Check.Pod.Name, "to be removed from node", w.Check.Node.Name)
	err = deletePodFromNode(ctx, w.Check.Pod, w.Check.Node)
	if err != nil {
		log.Errorln("Failed to remove pod", w.Check.Pod.Name, "from node", w.Check.Node.Name+":", err)
		errs = append(errs, err)
		// resultChan <- result
		return
	}
	log.Infoln("Pod", w.Check.Pod.Name, "deleted.")

	// // Report
	// resultChan <- result
}
