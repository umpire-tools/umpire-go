package umpire

import "fmt"

// dependencyGraph is the field dependency graph used for topological
// ordering. Mirrors the TS core's graph.ts.
type dependencyGraph struct {
	nodes          []string
	adjacency      map[string][]string
	incomingCounts map[string]int
}

func buildGraph(fieldOrder []string, rules []*Rule) (*dependencyGraph, error) {
	g := &dependencyGraph{
		nodes:          fieldOrder,
		adjacency:      map[string][]string{},
		incomingCounts: map[string]int{},
	}
	for _, node := range fieldOrder {
		g.adjacency[node] = nil
		g.incomingCounts[node] = 0
	}

	seenEdges := map[string]bool{}
	addEdge := func(from, to, ruleType string) {
		key := from + ":" + to + ":" + ruleType
		if seenEdges[key] {
			return
		}
		seenEdges[key] = true
		g.adjacency[from] = append(g.adjacency[from], to)
		g.incomingCounts[to]++
	}

	for _, rule := range rules {
		if rule.kind == "oneOf" {
			// Branch resolution is internal; oneOf creates no ordering edges.
			continue
		}
		ordering, _ := rule.graphSources()
		for _, source := range ordering {
			for _, target := range rule.targets {
				if source == target {
					continue
				}
				addEdge(source, target, rule.kind)
			}
		}
	}
	return g, nil
}

// detectCycles returns an error naming a cycle, if one exists.
func (g *dependencyGraph) detectCycles() error {
	visited := map[string]bool{}
	active := map[string]bool{}
	var path []string

	var visit func(node string) []string
	visit = func(node string) []string {
		visited[node] = true
		active[node] = true
		path = append(path, node)

		for _, next := range g.adjacency[node] {
			if !visited[next] {
				if cycle := visit(next); cycle != nil {
					return cycle
				}
				continue
			}
			if !active[next] {
				continue
			}
			start := -1
			for i, n := range path {
				if n == next {
					start = i
					break
				}
			}
			cycle := append(append([]string{}, path[start:]...), next)
			return cycle
		}

		path = path[:len(path)-1]
		active[node] = false
		return nil
	}

	for _, node := range g.nodes {
		if visited[node] {
			continue
		}
		if cycle := visit(node); cycle != nil {
			names := ""
			for i, n := range cycle {
				if i > 0 {
					names += " → "
				}
				names += n
			}
			return fmt.Errorf("[@umpire/core] Cycle detected: %s", names)
		}
	}
	return nil
}

// topologicalSort produces the evaluation order, seeding the queue in field
// order for deterministic output.
func (g *dependencyGraph) topologicalSort() ([]string, error) {
	incoming := map[string]int{}
	for _, f := range g.nodes {
		incoming[f] = g.incomingCounts[f]
	}

	var queue []string
	for _, f := range g.nodes {
		if incoming[f] == 0 {
			queue = append(queue, f)
		}
	}

	result := []string{}
	for index := 0; index < len(queue); index++ {
		node := queue[index]
		result = append(result, node)
		for _, next := range g.adjacency[node] {
			incoming[next]--
			if incoming[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(result) != len(g.nodes) {
		if err := g.detectCycles(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("[@umpire/core] Unable to produce topological order")
	}
	return result, nil
}
