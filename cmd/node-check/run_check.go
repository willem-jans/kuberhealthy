// Copyright 2018 Comcast Cable Communications Management, LLC
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	nodeCheck "github.com/Comcast/kuberhealthy/v2/pkg/checks/external/nodeCheck"
	log "github.com/sirupsen/logrus"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Check result represents the result of applying a pod to a node and its cleanup process.
type CheckInfo struct {
	Errors   []error
	OK       bool
	Node     *v1.Node
	NodeName string
	Pod      *v1.Pod
	PodName  string
}

// runNodeCheck applies a pod specification to each targeted node in the cluster.
// Determines which nodes are targeted for this check via environment variables for
// tolerations, labels, selectors, taints, name, etc. Each node will need to have the
// pod `Running` or `Completed` for 30s -- or the specified timelength before being
// considered successful and subsequently torn down.
func runNodeCheck(ctx context.Context) {

	log.Infoln("Waiting for node to become ready before starting check.")
	waitForNodeToJoin(ctx)

	log.Infoln("Starting check.")

	// Init a timeout for this entire check run.
	runTimeout := time.After(checkTimeLimit)

	// List targeted / qualifying nodes for this check.
	var nodeList NodeListResult
	select {
	case nodeList = <-listTargetedNodes(ctx):
		// Report node listing errors.
		if nodeList.Err != nil {
			log.Errorln("error when listing targeted or qualifying nodes:", nodeList.Err)
			reportOKToKuberhealthy()
			// reportErrorsToKuberhealthy([]string{nodeListResult.Err.Error()})
			return
		}
		// Exit the check if the slice of resulting nodes is empty.
		if len(nodeList.NodeList) == 0 {
			log.Infoln("There were no qualifying target nodes found within the cluster.")
			reportOKToKuberhealthy()
			return
		}
	case <-ctx.Done():
		// If there is a cancellation interrupt signal.
		log.Infoln("Canceling node target listing and shutting down from interrupt.")
		reportOKToKuberhealthy()
		// reportErrorsToKuberhealthy([]string{"failed to perform pre-check cleanup within timeout"})
		return
	case <-runTimeout:
		// If creating a deployment took too long, exit.
		reportOKToKuberhealthy()
		// reportErrorsToKuberhealthy([]string{"failed to create deployment within timeout"})
		return
	}
	log.Infoln("Found", len(nodeList.NodeList), "targeted nodes.")

	// List unschedulable nodes to skip.
	var unschedulableNodeList NodeListResult
	select {
	case unschedulableNodeList = <-listUnschedulableNodes(ctx):
		// Report node listing errors.
		if unschedulableNodeList.Err != nil {
			log.Errorln("error when listing unschedulable nodes:", unschedulableNodeList.Err)
			reportOKToKuberhealthy()
			// reportErrorsToKuberhealthy([]string{unschedulableNodeList.Err.Error()})
			return
		}
	case <-ctx.Done():
		// If there is a cancellation interrupt signal.
		log.Infoln("Canceling unschedulable node listing and shutting down from interrupt.")
		reportOKToKuberhealthy()
		// reportErrorsToKuberhealthy([]string{"failed to perform pre-check cleanup within timeout"})
		return
	case <-runTimeout:
		// If creating a deployment took too long, exit.
		reportOKToKuberhealthy()
		// reportErrorsToKuberhealthy([]string{"failed to create deployment within timeout"})
		return
	}
	log.Infoln("Found", len(unschedulableNodeList.NodeList), "unschedulable nodes.")

	// Trim the unschedaluable nodes from the list of targeted nodes.
	targetedNodes := removeUnscheduableNodes(&nodeList.NodeList, &unschedulableNodeList.NodeList)
	log.Infoln("There are now", len(targetedNodes), "targets after removing unschedulable candidates.")

	// Create pod specs for each targeted node.
	podSpecs := createPodSpecs(targetedNodes)
	if len(podSpecs) == 0 {
		// If there are no pod specifications to run for this check -- there is no work to do.
		// This could be both an error and a success case depending on the situation. . .
		log.Warnln("Produced an empty list of pod specifications for the check. Please check your inputs.")
		reportOKToKuberhealthy()
		// reportErrorsToKuberhealthy([]string{nodeListResult.Err.Error()})
		return
	}
	log.Infoln("Created", len(podSpecs), "pod specifications for nodes.")

	// Warn users that there is a discrepancy between the number of targeted nodes and the number
	// of created pod specs.
	if len(podSpecs) != len(targetedNodes) {
		log.Warnln("The number of created pod specifications and targeted nodes do not match. Targeted nodes:", len(targetedNodes), "Pods to create:", len(podSpecs))
	}

	// Make a list of test that are going to be run for this check.
	var checkTests []CheckInfo
	select {
	case err := <-populateCheckTests(&checkTests, targetedNodes, podSpecs):
		if err != nil {
			log.Errorln("failed to populate tests for this check:", err)
			reportOKToKuberhealthy()
			// reportErrorsToKuberhealthy([]string{err.Error()})
			return
		}
	case <-ctx.Done():
		// If there is a cancellation interrupt signal.
		log.Infoln("Canceling node target listing and shutting down from interrupt.")
		reportOKToKuberhealthy()
		// reportErrorsToKuberhealthy([]string{"failed to perform pre-check cleanup within timeout"})
		return
	case <-runTimeout:
		// If creating a deployment took too long, exit.
		reportOKToKuberhealthy()
		// reportErrorsToKuberhealthy([]string{"failed to create deployment within timeout"})
		return
	}

	// Deploy each pod spec to the cluster.
	var checkResults []CheckInfo
	select {
	case checkResults = <-executeNodeCheck(ctx, &checkTests):
		// Report node listing errors.
		if nodeList.Err != nil {
			log.Errorln("error when listing targeted or qualifying nodes:", nodeList.Err)
			reportOKToKuberhealthy()
			// reportErrorsToKuberhealthy([]string{nodeListResult.Err.Error()})
			return
		}
		// Exit the check if the slice of resulting nodes is empty.
		if len(nodeList.NodeList) == 0 {
			log.Infoln("There were no qualifying target nodes found within the cluster.")
			reportOKToKuberhealthy()
			return
		}
	case <-ctx.Done():
		// If there is a cancellation interrupt signal.
		log.Infoln("Canceling node target listing and shutting down from interrupt.")
		reportOKToKuberhealthy()
		// reportErrorsToKuberhealthy([]string{"failed to perform pre-check cleanup within timeout"})
		return
	case <-runTimeout:
		// If creating a deployment took too long, exit.
		reportOKToKuberhealthy()
		// reportErrorsToKuberhealthy([]string{"failed to create deployment within timeout"})
		return
	}

	// Create a report for kuberhealthy.
	var report []string
	select {
	case report = <-createKuberhealthyReport(&checkResults):
	case <-ctx.Done():
		// If there is a cancellation interrupt signal.
		log.Infoln("Canceling node target listing and shutting down from interrupt.")
		reportOKToKuberhealthy()
		// reportErrorsToKuberhealthy([]string{"failed to perform pre-check cleanup within timeout"})
		return
	case <-runTimeout:
		// If creating a deployment took too long, exit.
		reportOKToKuberhealthy()
		// reportErrorsToKuberhealthy([]string{"failed to create deployment within timeout"})
		return
	}

	// Report to Kuberhealthy.
	if len(report) != 0 {
		reportErrorsToKuberhealthy(report)
		return
	}
	reportOKToKuberhealthy()
}

// cleanUp cleans up the deployment check and all resource manifests created that relate to
// the check.
// TODO - add in context that expires when check times out
// func cleanUp(ctx context.Context) error {

// 	log.Infoln("Cleaning up deployment and service.")
// 	var err error
// 	var resultErr error
// 	errorMessage := ""

// 	// Delete the service.
// 	// TODO - add select to catch context timeout expiration
// 	err = deleteServiceAndWait(ctx)
// 	if err != nil {
// 		log.Errorln("error cleaning up service:", err)
// 		errorMessage = errorMessage + "error cleaning up service:" + err.Error()
// 	}

// 	// Delete the deployment.
// 	// TODO - add select to catch context timeout expiration
// 	err = deleteDeploymentAndWait(ctx)
// 	if err != nil {
// 		log.Errorln("error cleaning up deployment:", err)
// 		if len(errorMessage) != 0 {
// 			errorMessage = errorMessage + " | "
// 		}
// 		errorMessage = errorMessage + "error cleaning up deployment:" + err.Error()
// 	}

// 	log.Infoln("Finished clean up process.")

// 	// Create an error if errors occurred during the clean up process.
// 	if len(errorMessage) != 0 {
// 		resultErr = fmt.Errorf("%s", errorMessage)
// 	}

// 	return resultErr
// }

// populateCheckTests fills a slice of CheckResults with test information
// before the check (no results yet).
func populateCheckTests(checks *[]CheckInfo, nodes []v1.Node, podSpecs []v1.Pod) chan interface{} {
	completeChan := make(chan interface{})

	go func() {
		defer close(completeChan)

		// Apply pod specifications one at a time.
		for _, podSpec := range podSpecs {

			// Treat each pod specification as it's own check / test.
			// Add pod information
			check := CheckInfo{
				Errors:  make([]error, 0),
				Pod:     &podSpec,
				PodName: podSpec.GetName(),
			}

			// Find the node that the pod is assigned to be scheduled on
			node := findNodeInSlice(nodes, podSpec.Spec.NodeName)
			if node == nil {
				err := fmt.Errorf("unable to find targeted node for pod %s", podSpec.Name)
				check.Errors = append(check.Errors, err)
			}
			check.Node = node
			check.NodeName = node.GetName()

			*checks = append(*checks, check)
		}

		completeChan <- struct{}{}

		return
	}()

	return completeChan
}

// executeNodeCheck executes the deployment of pod specifications to nodes.
func executeNodeCheck(ctx context.Context, checks *[]CheckInfo) chan []CheckInfo {

	// Make a slice for the overall check results.
	results := make([]CheckInfo, 0)

	// Make a channel for overall check results.
	checkResults := make(chan []CheckInfo)

	// Make a channel to read check results from while running the check.
	checkResultProcessingChan := make(chan CheckInfo)

	go func() {
		defer close(checkResults)
		defer close(checkResultProcessingChan)

		// Perform the check concurrently
		wg := sync.WaitGroup{}
		for _, check := range *checks {
			w := NewWorker(check)
			wg.Add(1)
			go w.PerformCheck(ctx, checkResultProcessingChan, &wg)
		}

		for result := range checkResultProcessingChan {
			results = append(results, result)
		}

		wg.Wait()

		checkResults <- results

		return
	}()

	return checkResults
}

// checkNodesConcurrently executes the deployment of pod specifications to nodes concurrently.
func checkNodesConcurrently(ctx context.Context, resultChan chan []CheckInfo, nodes []v1.Node, podSpecs []v1.Pod) {
	results := make([]CheckInfo, 0)
	wg := sync.WaitGroup{}

	for _, _ = range nodes {
		wg.Add(1)
		// go
	}

	resultChan <- results
}

// checkNodes executes the deployment of pod specifications linearly. (One at a time)
func checkNodes(ctx context.Context, resultChan chan []CheckInfo, nodes []v1.Node, podSpecs []v1.Pod) {
	results := make([]CheckInfo, 0)

	// Apply pod specifications one at a time.
	for _, podSpec := range podSpecs {

		// Treat each pod specification as it's own check / test.
		// Add pod information
		checkResult := CheckInfo{
			Errors:  make([]error, 0),
			Pod:     &podSpec,
			PodName: podSpec.GetName(),
		}

		// Find the node that the pod is assigned to be scheduled on
		node := findNodeInSlice(nodes, podSpec.Spec.NodeName)
		if node == nil {
			err := fmt.Errorf("unable to find targeted node for pod %s", podSpec.Name)
			checkResult.Errors = append(checkResult.Errors, err)
		}
		checkResult.Node = node
		checkResult.NodeName = node.GetName()

		// Deploy the pod to the node
		pod, err := deployPodToNode(ctx, &podSpec, node)
		if err != nil {
			log.Errorln("failed to deploy pod", (*pod).Name, "to", node.Name)
			checkResult.Errors = append(checkResult.Errors, err)
		}
		checkResult.Pod = pod

		// Watch the pod and make sure it comes up.
		err = watchPodOnNode(ctx, checkResult.Pod, checkResult.Node)
		if err != nil {
			log.Errorln("failed to watch pod", checkResult.PodName, "come online on node", checkResult.NodeName)
			checkResult.Errors = append(checkResult.Errors, err)
		}

		// Delete the pod from the node
		err = deletePodFromNode(ctx, checkResult.Pod, checkResult.Node)
		if err != nil {
			log.Errorln("failed to delete pod", checkResult.PodName, "from node", checkResult.NodeName)
			checkResult.Errors = append(checkResult.Errors, err)
		}

		results = append(results, checkResult)
	}

	resultChan <- results
}

// watchPod watches for a pod to reach `Running` state on a node.
func watchPod(ctx context.Context, pod *v1.Pod) error {

	// Watch that the pod comes online
	watch, err := client.CoreV1().Pods(checkNamespace).Watch(ctx, metav1.ListOptions{
		Watch:         true,
		FieldSelector: "metadata.name=" + (*pod).Name,
	})
	if err != nil {
		log.Warnln("Failed to create a watch client on pod", (*pod).Name+":", err)
		return err
	}

	defer watch.Stop()

	for event := range watch.ResultChan() {
		kind := event.Object.DeepCopyObject().GetObjectKind().GroupVersionKind()
		if kind.Kind != "Pod" {
			log.Infoln("Got a watch event for a non-pod object.")
			log.Debugln(kind)
			continue
		}

		p, ok := event.Object.(*v1.Pod)
		if !ok {
			log.Infoln("Got a watch event for a non-pod object.")
			continue
		}

		if p.Status.Phase == v1.PodRunning {
			return nil
		}
	}

	return nil
}

// deletePodFromNode removes a pod from a node.
func deletePodFromNode(ctx context.Context, pod *v1.Pod, node *v1.Node) error {
	err := client.CoreV1().Pods(checkNamespace).Delete(ctx, (*pod).Name, metav1.DeleteOptions{})
	if err != nil {
		log.Debugln("failed to delete pod", (*pod).Name, "on node", (*node).Name+":", err.Error())
	}
	return err
}

// deployPodToNode deploys a given pod to a given node.
func deployPodToNode(ctx context.Context, pod *v1.Pod, node *v1.Node) (*v1.Pod, error) {
	p, err := client.CoreV1().Pods(checkNamespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		log.Debugln("failed to create pod", (*pod).Name, "on node", (*node).Name+":", err.Error())
		return nil, err
	}
	return p, nil
}

// cleanUpOrphanedResources cleans up previous deployment and services and ensures
// a clean slate before beginning a deployment and service check.
func cleanUpOrphanedResources(ctx context.Context) chan error {

	cleanUpChan := make(chan error)

	go func() {
		log.Infoln("Wiping all found orphaned resources belonging to this check.")

		defer close(cleanUpChan)

		// Look for existing pods based on time-stamp.
		// This should ignore all other targeting variables such as
		// tolerations, labels, selectors, tains, and such
		// as a change in these variables should not result in a faulty
		// cleanup -- unless this check is run in multiple namespaces???

		// svcExists, err := findPreviousService(ctx)
		// if err != nil {
		// 	log.Warnln("Failed to find previous service:", err.Error())
		// }
		// if svcExists {
		// 	log.Infoln("Found previous service.")
		// }

		// deploymentExists, err := findPreviousDeployment(ctx)
		// if err != nil {
		// 	log.Warnln("Failed to find previous deployment:", err.Error())
		// }
		// if deploymentExists {
		// 	log.Infoln("Found previous deployment.")
		// }

		// if svcExists || deploymentExists {
		// 	cleanUpChan <- cleanUp(ctx)
		// } else {
		// 	cleanUpChan <- nil
		// }
	}()

	return cleanUpChan
}

// createKuberhealthyReport creates and returns a report of errors from this check.
func createKuberhealthyReport(results *[]CheckInfo) chan []string {
	reportChan := make(chan []string)

	go func() {
		defer close(reportChan)

		report := make([]string, 0)

		for _, result := range *results {
			if len(result.Errors) != 0 {
				for _, err := range result.Errors {
					report = append(report, err.Error())
				}
			}
		}

		reportChan <- report

		return
	}()

	return reportChan
}

// waitForNodeToJoin waits for the node to join the worker pool.
// Waits for kube-proxy to be ready and that Kuberhealthy is reachable.
func waitForNodeToJoin(ctx context.Context) {
	// Check if Kuberhealthy is reachable.
	err := nodeCheck.WaitForKuberhealthy(ctx)
	if err != nil {
		log.Errorln("Failed to reach Kuberhealthy:", err.Error())
	}
}
