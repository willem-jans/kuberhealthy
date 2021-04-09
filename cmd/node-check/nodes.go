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

	log "github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var ()

const ()

type NodeListResult struct {
	Err      error
	NodeList []corev1.Node
}

type NodeInfo struct {
	Node          corev1.Node
	Unschedulable bool
}

// listTargetedNodes returns a list of targeted nodes depending on the
// given user inputs on what characteristics make a node qualify for this check.
func listTargetedNodes(ctx context.Context) chan NodeListResult {

	nodeListCompletedChan := make(chan NodeListResult)

	go func() {
		log.Infoln("Listing qualifying targeted nodes.")

		defer close(nodeListCompletedChan)

		result := NodeListResult{}

		// List qualifying nodes for a list of targets.
		nodeList, err := nodeClient.List(ctx, metav1.ListOptions{
			LabelSelector: checkNodeSelectorsEnv,
		})
		if err != nil {
			log.Errorf("Failed to list nodes in the cluster: %v\n", err)
			result.Err = err
			nodeListCompletedChan <- result
			return
		}
		if nodeList == nil {
			result.Err = errors.New("received a nil list of nodes: " + err.Error())
			log.Errorf("Failed to list nodes in the cluster: %v\n", err)
			nodeListCompletedChan <- result
			return
		}

		// Return the list of nodes.
		result.NodeList = nodeList.Items
		nodeListCompletedChan <- result
		return
	}()

	return nodeListCompletedChan
}

// removeUnscheduableNodes looks at a list of targetable nodes and creates a list of
// targetable nodes that are schedulable (this ignores `Unschedulable` nodes and
// `Cordoned` nodes).
// Returns a list of targetable, schedulable nodes.
func removeUnscheduableNodes(ctx context.Context) chan []NodeInfo {

}
