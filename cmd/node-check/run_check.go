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
	"errors"
	"fmt"
	"sync"
	"time"

	nodeCheck "github.com/Comcast/kuberhealthy/v2/pkg/checks/external/nodeCheck"
	log "github.com/sirupsen/logrus"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

// Check result represents the result of applying a pod to a node and its cleanup process.
type CheckInfo struct {
	Errors []error
	Node   *v1.Node
	Pod    *v1.Pod
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

	runErrs := make([]error, 0)

	// // Clean up pods from previous runs.
	// select {
	// case err := <-cleanUp(ctx):
	// 	if err != nil {
	// 		// Report and exit if cleaning up fails
	// 		log.Errorln("failed to clean up pods from runs that do not belong to this check iteration:", err)
	// 		reportErrorsToKuberhealthy(err)
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

	// List targeted / qualifying nodes for this check.
	var nodeList NodeListResult
	select {
	case nodeList = <-listTargetedNodes(ctx):
		// Report node listing errors.
		if nodeList.Err != nil {
			err := errors.New("failed to list targeted or qualifying nodes: " + nodeList.Err.Error())
			runErrs = append(runErrs, err)
			log.Errorln(err)
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
		// Debugging output
		log.Debugln("Number of targetable nodes:", len(nodeList.NodeList), "List of nodes:")
		if debug {
			for _, n := range nodeList.NodeList {
				log.Debugln(n.Name)
			}
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
			err := errors.New("failed to list unschedulable nodes: " + nodeList.Err.Error())
			runErrs = append(runErrs, err)
			log.Errorln(err)
			reportOKToKuberhealthy()
			// reportErrorsToKuberhealthy([]string{unschedulableNodeList.Err.Error()})
			return
		}
		// Debugging output
		log.Debugln("Number of unschedulable nodes:", len(nodeList.NodeList), "List of nodes:")
		if debug {
			for _, n := range unschedulableNodeList.NodeList {
				log.Debugln(n.Name)
			}
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
	log.Infoln("Found", len(unschedulableNodeList.NodeList), "unschedulable node(s).")

	// PROBLEM:
	// Trim the unschedaluable nodes from the list of targeted nodes.
	targetedNodes := removeUnscheduableNodesv2(&nodeList.NodeList, &unschedulableNodeList.NodeList)
	log.Infoln("Removed", len(nodeList.NodeList)-len(targetedNodes), "node(s) from the list of targets.")
	log.Infoln("Number of targets after removing unschedulable candidates:", len(targetedNodes))

	// // DEBUG ONLY
	// if debug {
	// 	log.Debugln("Targeted nodes:")
	// 	for _, n := range targetedNodes {
	// 		log.Debugln(n.Name, n.GetName())
	// 	}
	// }

	// log.Infoln("Removed", len(nodeList.NodeList)-len(*targetedNodes), "node(s) from the list of targets.")
	// log.Infoln("Number of targets after removing unschedulable candidates:", len(*targetedNodes))

	// // DEBUG ONLY
	// if debug {
	// 	log.Debugln("Targeted nodes:")
	// 	for _, n := range *targetedNodes {
	// 		log.Debugln(n.Name, n.GetName())
	// 	}
	// }

	// Execute node check here and move create pod spec to worker
	// Deploy each pod spec to the cluster.
	var checkResults []*CheckInfo
	select {
	// case checkResults = <-executeNodeCheckv2(ctx, targetedNodes):
	case checkResults = <-executeNodeCheckv3(ctx, &targetedNodes):
		// Report node listing errors.
		// if len(checkResults) != len(*targetedNodes) {
		if len(checkResults) != len(targetedNodes) {
			err := fmt.Errorf("failed to produce the proper amount of check results: [expected]: %d [received]: %d", len(targetedNodes), len(checkResults))
			runErrs = append(runErrs, err)
			log.Errorln(err)
			reportOKToKuberhealthy()
			// reportErrorsToKuberhealthy([]string{nodeListResult.Err.Error()})
			return
		}
		// Debugging output
		if debug {
			log.Debugln("Number of worker results:", len(checkResults), "List of nodes with results:")
			for _, r := range checkResults {
				log.Debugln(r.Node.GetName())
			}
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
	log.Infoln("Check completed.")

	// podSpecs := createPodSpecs(*targetedNodes)
	// if len(podSpecs) == 0 {
	// 	// If there are no pod specifications to run for this check -- there is no work to do.
	// 	// This could be both an error and a success case depending on the situation. . .
	// 	log.Warnln("Produced an empty list of pod specifications for the check. Please check your inputs.\n\tCheck that your selected nodes are not Unschedulable.")
	// 	reportOKToKuberhealthy()
	// 	// reportErrorsToKuberhealthy([]string{nodeListResult.Err.Error()})
	// 	return
	// }
	// log.Infoln("Created", len(podSpecs), "pod specification(s).")

	// // Warn users that there is a discrepancy between the number of targeted nodes and the number
	// // of created pod specs.
	// if len(podSpecs) != len(*targetedNodes) {
	// 	log.Warnln("The number of created pod specifications and targeted nodes do not match. Node(s):", len(*targetedNodes), "Pod(s):", len(podSpecs))
	// }

	// // Make a list of test that are going to be run for this check.
	// var checkTests []*CheckInfo
	// select {
	// case tests := <-populateCheckTests(*targetedNodes, podSpecs):
	// 	if len(tests) != len(*targetedNodes) {
	// 		log.Errorln("failed to populate the appropriate amount of tests for this check [expected]:", len(*targetedNodes), "[populated]:", len(checkTests))
	// 		reportOKToKuberhealthy()
	// 		// reportErrorsToKuberhealthy([]string{err.Error()})
	// 		return
	// 	}
	// 	checkTests = tests
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

	// // Still the breaking point in the check
	// log.Infoln("Created", len(checkTests), "tests for workers.")
	// if debug {
	// 	for _, c := range checkTests {
	// 		log.Debugln("Pod", c.Pod.Name, "maps to", c.Pod.Spec.NodeName)
	// 	}
	// }

	// // Deploy each pod spec to the cluster.
	// var checkResults []*CheckInfo
	// select {
	// case checkResults = <-executeNodeCheck(ctx, &checkTests):
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
	// log.Infoln("Check completed.")

	log.Infoln("Creating report from the results.")
	// Create a report for kuberhealthy.
	var report []string
	select {
	case report = <-createKuberhealthyReportv2(&checkResults, runErrs):
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

// populateCheckTests fills a slice of CheckResults with test information
// before the check (no results yet).
func populateCheckTests(nodes []*v1.Node, podSpecs []*v1.Pod) chan []*CheckInfo {
	completeChan := make(chan []*CheckInfo)
	checkProcessingChan := make(chan *CheckInfo)

	checks := make([]*CheckInfo, 0)

	go readCheckCreations(checkProcessingChan, &checks)

	go func() {
		defer close(completeChan)

		// Create pod specifications one at a time.
		for _, podSpec := range podSpecs {

			log.Debugln("Creating check for", podSpec.Name, "on node", podSpec.Spec.NodeName)

			// Treat each pod specification as it's own check / test.
			// Add pod information
			check := CheckInfo{
				Errors: make([]error, 0),
				Pod:    podSpec,
				// PodName: podSpec.GetName(),
			}

			// Find the node that the pod is assigned to be scheduled on
			node := findNodeInSlice(nodes, podSpec.Spec.NodeName)
			if node == nil {
				err := fmt.Errorf("unable to find targeted node for pod %s", podSpec.Name)
				check.Errors = append(check.Errors, err)
			}

			check.Node = node
			// check.NodeName = node.GetName()

			// checks = append(checks, &check)

			// log.Debugln(podSpec.Name)
			// log.Debugln(node.Name)
			// log.Debugln(check.Pod.Name, check.PodName, check.Pod.Spec.NodeName, check.Node.Name, check.NodeName)

			log.Debugln("Sending check creation for", check.Pod.Name, "on node", check.Node.Name, "to processing channel.")
			checkProcessingChan <- &check
			// time.Sleep(time.Millisecond)
			// log.Debugln(checks)
		}

		close(checkProcessingChan)

		// if debug {
		// 	for _, c := range checks {
		// 		log.Debugln("Pod.Name:", c.Pod.Name)
		// 		log.Debugln("Pod.GetName():", c.Pod.GetName())
		// 		log.Debugln("Pod.Spec.NodeName:", c.Pod.Spec.NodeName)
		// 		log.Debugln("Pod.Spec.NodeSelector:", c.Pod.Spec.NodeSelector)
		// 		log.Debugln("Node.Name:", c.Node.Name)
		// 		log.Debugln("Node.Name:", c.Node.GetName())
		// 		log.Debugln("Node.Labels:", c.Node.Labels)
		// 	}
		// }

		if debug {
			for _, c := range checks {
				log.Debugln("Pod", c.Pod.Name, "maps to", c.Node.Name)
			}
		}

		completeChan <- checks

		return
	}()

	return completeChan
}

// executeNodeCheck executes the deployment of pod specifications to nodes.
func executeNodeCheck(ctx context.Context, checks *[]*CheckInfo) chan []*CheckInfo {

	// Make a channel for overall check results.
	checkResults := make(chan []*CheckInfo)

	// Make a channel to read check results from while running the check.
	checkResultReadingChan := make(chan []*CheckInfo)
	checkResultProcessingChan := make(chan *CheckInfo)

	go func() {
		defer close(checkResults)
		defer close(checkResultProcessingChan)

		go readCheckResults(checkResultReadingChan, checkResultProcessingChan)

		// Perform the check concurrently
		wg := sync.WaitGroup{}
		for _, check := range *checks {
			w := NewWorker(check)
			wg.Add(1)
			go w.PerformCheck(ctx, checkResultProcessingChan, &wg)
		}

		wg.Wait()

		results := <-checkResultReadingChan
		checkResults <- results

		return
	}()

	return checkResults
}

// executeNodeCheckv2
func executeNodeCheckv2(ctx context.Context, nodes *[]*v1.Node) chan []*CheckInfo {

	// Make a channel to read check results from while running the check.
	checkResultReadingChan := make(chan []*CheckInfo)
	checkResultProcessingChan := make(chan *CheckInfo)

	checkResults := make(chan []*CheckInfo)

	go func() {
		defer close(checkResults)
		// defer close(checkResultProcessingChan)
		// defer close(checkResults)
		// results := make([]*CheckInfo, 0)

		// Start reading check results.
		go readCheckResults(checkResultReadingChan, checkResultProcessingChan)

		// Perform the check concurrently
		wg := sync.WaitGroup{}
		for _, node := range *nodes {
			// Add a worker for the check.
			w := NewWorkerv2(node)
			wg.Add(1)
			go w.PerformCheckv2(ctx, checkResultProcessingChan, &wg)
		}

		// Wait for workers to finish.
		wg.Wait()

		// Close the processing channel when workers are done.
		close(checkResultProcessingChan)

		// Read the results and send it back to the check result channel.
		results := <-checkResultReadingChan
		checkResults <- results
	}()

	return checkResults
}

// executeNodeCheckv3
func executeNodeCheckv3(ctx context.Context, nodes *[]v1.Node) chan []*CheckInfo {

	log.Debugln("Executing node checks.")

	// Make a channel to read check results from while running the check.
	checkResultReadingChan := make(chan []*CheckInfo)
	checkResultProcessingChan := make(chan *CheckInfo)

	checkResults := make(chan []*CheckInfo)

	go func() {
		defer close(checkResults)
		// defer close(checkResultProcessingChan)
		// defer close(checkResults)
		// results := make([]*CheckInfo, 0)

		// Start reading check results.
		go readCheckResults(checkResultReadingChan, checkResultProcessingChan)

		// Perform the check concurrently
		wg := sync.WaitGroup{}
		log.Debugln("Amount of workers to make for nodes:", len(*nodes))
		for _, node := range *nodes {
			log.Debugln("About to initialize worker for node", node.Name)
			// Add a worker for the check.
			w := NewWorkerv2(&node)
			wg.Add(1)
			log.Debugln("Starting worker for ", w.Check.Node.GetName())
			go w.PerformCheckv2(ctx, checkResultProcessingChan, &wg)

			// Sleep here to prevent workers from DOSing.
			log.Debugln("Sleeping for", defaultWorkerInterlude, "before spawning next worker.")
			time.Sleep(time.Second * time.Duration(defaultWorkerInterlude))
			// time.Sleep(time.Second * time.Duration(workerInterlude))
		}

		// Wait for workers to finish.
		wg.Wait()
		log.Infoln("Done waiting for workers to complete.")

		// Close the processing channel when workers are done.
		close(checkResultProcessingChan)

		// Read the results and send it back to the check result channel.
		results := <-checkResultReadingChan
		log.Debugln("Sending check results.")
		checkResults <- results
	}()

	return checkResults
}

// watchPodRunning watches for a pod to reach `Running` state on a node.
func watchPodRunning(ctx context.Context, pod *v1.Pod) error {

	// Watch that the pod comes online
	watch, err := client.CoreV1().Pods(checkNamespace).Watch(ctx, metav1.ListOptions{
		Watch:         true,
		FieldSelector: "metadata.name=" + pod.Name,
	})
	if err != nil {
		log.Warnln("Failed to create a watch client on pod", pod.Name+":", err)
		return err
	}

	defer watch.Stop()

	for event := range watch.ResultChan() {
		kind := event.Object.DeepCopyObject().GetObjectKind().GroupVersionKind()
		if kind.Kind != pod.TypeMeta.Kind {
			continue
		}

		p, ok := event.Object.(*v1.Pod)
		if !ok {
			log.Debugln("Got a watch event for a non-pod object:", p)
			continue
		}

		if p.GetName() != pod.GetName() {
			continue
		}

		log.Debugln("Recieved event for pod:", p.Name)

		for _, condition := range p.Status.Conditions {
			if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue && p.Status.Phase == v1.PodRunning {
				log.Debugln("Pod", p.GetName(), "is in phase", p.Status.Phase, "with condition", condition.Type, condition.Status)
				return nil
			}
		}

		// if p.Status.Phase == v1.PodRunning && p.Status.Conditions == v1.PodReady {

		// 	log.Debugln("Pod", p.GetName(), "is in phase", p.Status.Phase)
		// 	return nil
		// }
	}

	return nil
}

// watchPodRunningv2 watches for a pod to reach `Running` state on a node.
func watchPodRunningv2(ctx context.Context, pod *v1.Pod) error {

	// Watch that the pod comes online
	watch, err := client.CoreV1().Pods(checkNamespace).Watch(ctx, metav1.ListOptions{
		Watch:         true,
		FieldSelector: "metadata.name=" + pod.Name,
	})
	if err != nil {
		log.Warnln("Failed to create a watch client on pod", pod.Name+":", err)
		return err
	}

	defer watch.Stop()

	for event := range watch.ResultChan() {
		kind := event.Object.DeepCopyObject().GetObjectKind().GroupVersionKind()
		if kind.Kind != pod.TypeMeta.Kind {
			continue
		}

		p, ok := event.Object.(*v1.Pod)
		if !ok {
			log.Debugln("Got a watch event for a non-pod object:", p)
			continue
		}

		log.Debugln("Recieved event for pod:", p.Name)

		if p.Status.Phase == v1.PodRunning {
			return nil
		}
	}

	return nil
}

// watchPodDeleted watches for a pod to reach `Running` state on a node.
func watchPodDeleted(ctx context.Context, pod *v1.Pod) error {

	deleted := make(chan struct{})
	defer close(deleted)

	// Watch for a deleted even on the pod
	podInformer := informers.NewSharedInformerFactory(client, time.Second*5).Core().V1().Pods().Informer()
	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			// log.Debugln("Pod deleted:", obj)
			// log.Debugln("Found delete while watching pod.")
			p, ok := obj.(*v1.Pod)
			if !ok {
				log.Debugln("Found a deletion for a non-pod object")
			}
			if p.GetName() == pod.Name {
				log.Debugln("Pod deleted:", obj)
				return
			}
		},
	})

	podInformer.Run(deleted)

	select {
	case _ = <-deleted:
		log.Infoln("Pod", pod.Name, "deleted.")

	case <-ctx.Done():
		// If there is a cancellation interrupt signal.
		return errors.New("Canceling pod " + pod.Name + " delete watch.")
	}
	return nil
}

// deletePodFromNode removes a pod from a node.
func deletePodFromNode(ctx context.Context, pod *v1.Pod, node *v1.Node) error {
	err := client.CoreV1().Pods(checkNamespace).Delete(ctx, (*pod).Name, metav1.DeleteOptions{})
	if err != nil {
		log.Debugln("failed to delete pod", (*pod).Name, "on node", (*node).Name+":", err.Error())
		return err
	}
	err = watchPodDeleted(ctx, pod)
	if err != nil {
		log.Debugln("failed to watch deletion of pod", (*pod).Name, "on node", (*node).Name+":", err.Error())
		return err
	}
	return nil
}

// deployPodToNode deploys a given pod to a given node.
func deployPodToNode(ctx context.Context, pod *v1.Pod, node *v1.Node) (*v1.Pod, error) {
	log.Debugln("DEPLOY POD TO NODE Pod:", pod.Name, "Node:", node.Name)
	p, err := client.CoreV1().Pods(checkNamespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		log.Debugln("failed to create pod", pod.Name, "on node", node.Name+":", err.Error())
		return nil, err
	}
	return p, nil
}

// deployPodToNodev2 deploys a given pod to a given node.
func deployPodToNodev2(ctx context.Context, pod *v1.Pod) (*v1.Pod, error) {
	log.Debugln("DEPLOY POD TO NODE Pod:", pod.Name, "Node:", pod.Spec.NodeName)
	p, err := client.CoreV1().Pods(checkNamespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		log.Debugln("failed to create pod", pod.Name, "on node", pod.Spec.NodeName+":", err.Error())
		return nil, err
	}
	return p, nil
}

// readCheckResults reads a channel of incoming check results and adds them to the
// list of overall results.
func readCheckResults(allResults chan []*CheckInfo, individualResult chan *CheckInfo) {
	results := make([]*CheckInfo, 0)
	for r := range individualResult {
		results = append(results, r)
	}
	allResults <- results
}

// readCheckCreations reads a channel of created check tests and adds them to the
// list of overall tests.
func readCheckCreations(createdCheckChan chan *CheckInfo, createdChecks *[]*CheckInfo) {
	for check := range createdCheckChan {
		log.Debugln("Reading check creation for", check.Pod.Name, "on node", check.Node.Name, "from result channel.")
		*createdChecks = append(*createdChecks, check)
	}
}

// createKuberhealthyReport creates and returns a report of errors from this check.
func createKuberhealthyReport(results *[]*CheckInfo) chan []string {
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

// createKuberhealthyReportv2 creates and returns a report of errors from this check.
func createKuberhealthyReportv2(results *[]*CheckInfo, runErrs []error) chan []string {
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

		for _, err := range runErrs {
			report = append(report, err.Error())
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
