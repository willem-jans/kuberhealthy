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

	log "github.com/sirupsen/logrus"
)

// CheckWorker represents a worker for a specific pod and node.
type CheckWorker struct {
	Check CheckInfo
}

// NewWorker returns a new worker with its given check task.
// func NewWorker(ctx context.Context, check CheckInfo) CheckWorker {
func NewWorker(check CheckInfo) CheckWorker {
	return CheckWorker{
		Check: check,
	}
}

// PerformCheck makes the worker perform its assigned check.
func (w CheckWorker) PerformCheck(ctx context.Context, resultChan chan CheckInfo, wg *sync.WaitGroup) {

	defer wg.Done()

	result := w.Check

	// Apply
	pod, err := deployPodToNode(ctx, w.Check.Pod, w.Check.Node)
	if err != nil {
		log.Errorln("Worker failed to deploy pod", w.Check.PodName, "to node", w.Check.NodeName)
		result.Errors = append(result.Errors, err)
		return
	}
	// select {
	// case nodeList = <-deployPodToNode(ctx):
	// 	// Report node listing errors.
	// 	if nodeList.Err != nil {
	// 		log.Errorln("error when listing targeted or qualifying nodes:", nodeList.Err)
	// 		reportOKToKuberhealthy()
	// 		// reportErrorsToKuberhealthy([]string{nodeListResult.Err.Error()})
	// 		return
	// 	}
	// 	// Exit the check if the slice of resulting nodes is empty.
	// 	if len(nodeList.NodeList) == 0 {
	// 		log.Infoln("There were no qualifying target nodes found within the cluster.")
	// 		reportOKToKuberhealthy()
	// 		return
	// 	}
	// case <-ctx.Done():
	// 	// If there is a cancellation interrupt signal.
	// 	log.Infoln("Canceling node target listing and shutting down from interrupt.")
	// 	reportOKToKuberhealthy()
	// 	// reportErrorsToKuberhealthy([]string{"failed to perform pre-check cleanup within timeout"})
	// 	return
	// case <-runTimeout:
	// 	// If creating a deployment took too long, exit.
	// 	reportOKToKuberhealthy()
	// 	// reportErrorsToKuberhealthy([]string{"failed to create deployment within timeout"})
	// 	return
	// }
	// Watch
	// Delete
	// Watch
	// Report
}
