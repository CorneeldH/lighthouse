// Package prioweight computes the priority weight of an issue (rule of #445):
// its own milestone factor plus, for every issue waiting on it, directly or
// transitively, the factor of that issue times 2^(k-1), with k the longest
// distance in steps.
package prioweight

import "sort"

// Issue is the input of Compute. Milestone is the milestone number, 0 without
// one. BlockedBy holds only issue numbers of the same repo.
type Issue struct {
	Number    int
	Milestone int
	BlockedBy []int
}

// Factors turns milestone number -> rank (only milestones with a rank) into
// milestone number -> factor. The factor follows the position in the ascending
// ranks: M minus the 0-based index of the first occurrence of that rank, so a
// gap in the ranks gives no negative factor and equal ranks share a factor.
func Factors(ranks map[int]int) map[int]int {
	sorted := make([]int, 0, len(ranks))
	for _, r := range ranks {
		sorted = append(sorted, r)
	}
	sort.Ints(sorted)
	first := make(map[int]int, len(sorted))
	for i, r := range sorted {
		if _, ok := first[r]; !ok {
			first[r] = i
		}
	}
	out := make(map[int]int, len(ranks))
	for ms, r := range ranks {
		out[ms] = len(sorted) - first[r]
	}
	return out
}

// Compute returns issue number -> priority weight for every given issue. A
// milestone without a factor counts as 0; a blocker outside issues is ignored;
// a cycle is condensed to one group whose members wait on each other at
// distance 1.
func Compute(issues []Issue, factors map[int]int) map[int]int {
	index := make(map[int]int, len(issues))
	for i, is := range issues {
		index[is.Number] = i
	}
	// waiters[b] = issues that wait on b (reverse graph), without duplicates.
	waiters := make([][]int, len(issues))
	seen := make(map[[2]int]bool)
	for i, is := range issues {
		for _, b := range is.BlockedBy {
			j, ok := index[b]
			if !ok || j == i || seen[[2]int{j, i}] {
				continue
			}
			seen[[2]int{j, i}] = true
			waiters[j] = append(waiters[j], i)
		}
	}

	comp, ncomp := scc(waiters)
	members := make([][]int, ncomp)
	for i, c := range comp {
		members[c] = append(members[c], i)
	}
	// Edges between components.
	next := make([]map[int]bool, ncomp)
	for c := range next {
		next[c] = map[int]bool{}
	}
	for i, ws := range waiters {
		for _, w := range ws {
			if comp[i] != comp[w] {
				next[comp[i]][comp[w]] = true
			}
		}
	}
	// dist[c][d] = longest distance in steps from component c to d over the DAG.
	dist := make([]map[int]int, ncomp)
	var longest func(c int) map[int]int
	longest = func(c int) map[int]int {
		if dist[c] != nil {
			return dist[c]
		}
		m := map[int]int{}
		for e := range next[c] {
			if m[e] < 1 {
				m[e] = 1
			}
			for d, k := range longest(e) {
				if m[d] < k+1 {
					m[d] = k + 1
				}
			}
		}
		dist[c] = m
		return m
	}

	factor := func(i int) int { return factors[issues[i].Milestone] }
	out := make(map[int]int, len(issues))
	for i, is := range issues {
		w := factor(i)
		for _, m := range members[comp[i]] {
			if m != i {
				w += factor(m)
			}
		}
		for d, k := range longest(comp[i]) {
			for _, m := range members[d] {
				w += factor(m) << (k - 1)
			}
		}
		out[is.Number] = w
	}
	return out
}

// scc labels the strongly connected components of the graph (Tarjan) and
// returns the component per node and the number of components.
func scc(adj [][]int) ([]int, int) {
	n := len(adj)
	idx := make([]int, n)
	low := make([]int, n)
	on := make([]bool, n)
	comp := make([]int, n)
	for i := range idx {
		idx[i] = -1
	}
	var stack []int
	counter, ncomp := 0, 0
	var visit func(v int)
	visit = func(v int) {
		idx[v], low[v] = counter, counter
		counter++
		stack = append(stack, v)
		on[v] = true
		for _, w := range adj[v] {
			if idx[w] == -1 {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if on[w] {
				low[v] = min(low[v], idx[w])
			}
		}
		if low[v] == idx[v] {
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				on[w] = false
				comp[w] = ncomp
				if w == v {
					break
				}
			}
			ncomp++
		}
	}
	for v := range adj {
		if idx[v] == -1 {
			visit(v)
		}
	}
	return comp, ncomp
}
