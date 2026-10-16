package main

import (
	"fmt"
	"strconv"
)

// nodeDetail is the secondary line under a node's name in the list.
func nodeDetail(n Node) string {
	return fmt.Sprintf("%s · %s:%s · %s", n.Protocol, n.Server, n.Port, n.Transport)
}

func nodeNames(nodes []Node) []string {
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		names = append(names, n.Name)
	}
	return names
}

func indexOfNode(nodes []Node, name string) int {
	for i, n := range nodes {
		if n.Name == name {
			return i
		}
	}
	return -1
}

func removeNode(nodes []Node, name string) []Node {
	out := nodes[:0:0]
	for _, n := range nodes {
		if n.Name != name {
			out = append(out, n)
		}
	}
	return out
}

func replaceNode(nodes []Node, name string, with Node) []Node {
	for i := range nodes {
		if nodes[i].Name == name {
			nodes[i] = with
			break
		}
	}
	return nodes
}

func validPort(s string) bool {
	p, err := strconv.Atoi(s)
	return err == nil && p >= 1 && p <= 65535
}
