package main

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestContainsNodeName(t *testing.T) {
	nodePool := []v1.Node{
		v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
			},
		},
		v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-a",
			},
		},
		v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-node",
			},
		},
		v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-test",
			},
		},
		v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "z",
			},
		},
	}

	tests := []struct {
		nodes    []v1.Node
		node     v1.Node
		expected bool
	}{
		{
			nodePool,
			v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "a",
				},
			},
			false,
		},
		{
			nodePool,
			v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "0",
				},
			},
			false,
		},
		{
			nodePool,
			v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-test",
				},
			},
			true,
		},
		{
			nodePool,
			v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "z",
				},
			},
			true,
		},
	}

	for _, test := range tests {
		result := containsNodeName(test.nodes, &test.node)
		if test.expected != result {
			t.Errorf("failed to check if node list contains a node, expected %t but got %t", test.expected, result)
		}
	}
}

func TestFindNodeInSlice(t *testing.T) {

}
