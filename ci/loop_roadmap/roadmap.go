package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

const (
	label  = "loop"
	hint   = "(see docs/developer/labels.md)"
	mapRel = "docs/site/data/loop.json"
)

// Chain is the part of the loop map this check reads: the chips and the handoffs.
type Chain struct {
	Chips    []chip
	Handoffs []handoff
}

type chip struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Issue  *int   `json:"issue"`
}

type handoff struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"`
	Issue  *int   `json:"issue"`
}

// Item is a planned item of the map by name, with the issue it names (nil: no issue).
type Item struct {
	Name  string
	Issue *int
}

// parseChain reads loop.json; a missing id, from, to or status is an error, as the original's
// KeyError was.
func parseChain(data []byte) (Chain, error) {
	var raw struct {
		Chips    *[]chip    `json:"chips"`
		Handoffs *[]handoff `json:"handoffs"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Chain{}, err
	}
	if raw.Chips == nil || raw.Handoffs == nil {
		return Chain{}, errors.New("chips and handoffs are both required")
	}
	c := Chain{Chips: *raw.Chips, Handoffs: *raw.Handoffs}
	for i, ch := range c.Chips {
		if ch.ID == "" || ch.Status == "" {
			return Chain{}, fmt.Errorf("chip %d has no id or status", i)
		}
	}
	for i, h := range c.Handoffs {
		if h.From == "" || h.To == "" || h.Status == "" {
			return Chain{}, fmt.Errorf("handoff %d has no from, to or status", i)
		}
	}
	return c, nil
}

// Planned lists the planned items (status other than works), chips first and then handoffs. A
// name that occurs twice keeps its first position and its last issue, like a Python dict.
func Planned(chain Chain) []Item {
	var all []Item
	var works []bool
	for _, c := range chain.Chips {
		all = append(all, Item{c.ID, c.Issue})
		works = append(works, c.Status == "works")
	}
	for _, h := range chain.Handoffs {
		all = append(all, Item{h.From + "->" + h.To, h.Issue})
		works = append(works, h.Status == "works")
	}
	index := map[string]int{}
	var items []Item
	var itemWorks []bool
	for i, it := range all {
		if at, ok := index[it.Name]; ok {
			items[at], itemWorks[at] = it, works[i]
			continue
		}
		index[it.Name] = len(items)
		items = append(items, it)
		itemWorks = append(itemWorks, works[i])
	}
	var planned []Item
	for i, it := range items {
		if !itemWorks[i] {
			planned = append(planned, it)
		}
	}
	return planned
}

// Problems lists planned items without an open `loop` issue, and milestoned open `loop` issues
// not on the map. `open` maps an issue number to its milestone title ("": none).
func Problems(chain Chain, open map[int]string) []string {
	items := Planned(chain)
	var found []string
	for _, it := range items {
		if it.Issue == nil {
			found = append(found, it.Name+": planned without an issue")
		}
	}
	for _, it := range items {
		if it.Issue != nil {
			if _, ok := open[*it.Issue]; !ok {
				found = append(found, fmt.Sprintf("%s: #%d is not an open issue labelled %s %s", it.Name, *it.Issue, label, hint))
			}
		}
	}
	onMap := map[int]bool{}
	for _, it := range items {
		if it.Issue != nil {
			onMap[*it.Issue] = true
		}
	}
	numbers := make([]int, 0, len(open))
	for nr := range open {
		numbers = append(numbers, nr)
	}
	slices.Sort(numbers)
	for _, nr := range numbers {
		if title := open[nr]; title != "" && !onMap[nr] {
			found = append(found, fmt.Sprintf("#%d: open issue labelled %s on milestone %s is not on the map %s", nr, label, title, hint))
		}
	}
	return found
}

// PRProblems lists what a PR closing the `loop` issues `closed` leaves undone on the map.
func PRProblems(chain Chain, closed map[int]bool, touched map[string]bool) []string {
	var found []string
	reported := map[int]bool{}
	for _, it := range Planned(chain) {
		if it.Issue != nil && closed[*it.Issue] {
			reported[*it.Issue] = true
			found = append(found, fmt.Sprintf("%s: still planned, but this PR closes #%d; set status works and drop issue %s", it.Name, *it.Issue, hint))
		}
	}
	if !touched[mapRel] {
		var rest []int
		for nr := range closed {
			if !reported[nr] {
				rest = append(rest, nr)
			}
		}
		slices.Sort(rest)
		for _, nr := range rest {
			found = append(found, fmt.Sprintf("#%d: this PR closes a loop issue but leaves %s untouched %s", nr, mapRel, hint))
		}
	}
	return found
}
