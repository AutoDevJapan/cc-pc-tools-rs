package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode"
)

// Private distribution supplies these with -ldflags -X, never public CI values.
var mcpContractRepository, mcpContractOwner, mcpAlphaRepository, mcpAlphaOwner string

type productBinding struct {
	Repository uint64 `json:"repository_id"`
	Owner      uint64 `json:"owner_id"`
}
type productIdentity struct {
	ProductID string `json:"product_id,omitempty"`
	State     string `json:"state"`
}
type mcpResolution struct {
	id, state string
	rank      int
	scoped    bool
}

func mcpUnknown() mcpResolution { return mcpResolution{state: "unknown", rank: 100, scoped: true} }
func mcpProduct(id, state string) mcpResolution {
	rank := 60
	if id == "godd-mcp-contract" {
		rank = 10
	}
	return mcpResolution{id, state, rank, true}
}
func (r mcpResolution) projection() *productIdentity {
	if !r.scoped {
		return nil
	}
	return &productIdentity{r.id, r.state}
}
func (r mcpResolution) label() label {
	if r.id == "godd-mcp-contract" {
		return label{"受託案件", "generic", "受託部", 10}
	}
	if r.id == "godd-mcp-alpha" {
		return label{"GoDD-MCP-Alpha", "public", "GoDD事業部", 60}
	}
	return label{"名称未確認", "generic", rankDepartment(100), 100}
}
func mcpNameKey(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("-_:.", r) {
			return -1
		}
		return r
	}, strings.ReplaceAll(strings.ToLower(s), "α", "alpha"))
}
func mcpNamed(s string) mcpResolution {
	switch mcpNameKey(s) {
	case "goddmcp":
		return mcpProduct("godd-mcp-contract", "name_only")
	case "goddmcpalpha":
		return mcpProduct("godd-mcp-alpha", "name_only")
	}
	if strings.HasPrefix(mcpNameKey(s), "goddmcp") {
		return mcpUnknown()
	}
	return mcpResolution{}
}
func mcpBinding(id string, p priorityPolicy) (productBinding, bool) {
	if p.ProductBindingsPresent || p.ProductIdentities != nil {
		b, ok := p.ProductIdentities[id]
		return b, ok && b.Repository > 0 && b.Owner > 0
	}
	repository, owner := mcpAlphaRepository, mcpAlphaOwner
	if id == "godd-mcp-contract" {
		repository, owner = mcpContractRepository, mcpContractOwner
	}
	r, errR := strconv.ParseUint(repository, 10, 64)
	o, errO := strconv.ParseUint(owner, 10, 64)
	return productBinding{r, o}, errR == nil && errO == nil && r > 0 && o > 0
}
func mcpBindingsConfigured() bool {
	contract, a := mcpBinding("godd-mcp-contract", priorityPolicy{})
	alpha, b := mcpBinding("godd-mcp-alpha", priorityPolicy{})
	return a && b && contract.Repository != alpha.Repository
}
func mcpExplicit(raw json.RawMessage, p priorityPolicy) mcpResolution {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return mcpUnknown()
	}
	var repository, owner uint64
	if json.Unmarshal(fields["repository_id"], &repository) != nil || json.Unmarshal(fields["owner_id"], &owner) != nil {
		return mcpUnknown()
	}
	for _, id := range []string{"godd-mcp-contract", "godd-mcp-alpha"} {
		b, ok := mcpBinding(id, p)
		if !ok || b.Repository != repository {
			continue
		}
		if b.Owner != owner {
			return mcpUnknown()
		}
		if name, present := fields["product_id"]; present {
			s, ok := optionalString(name)
			if !ok || s != id {
				return mcpUnknown()
			}
		}
		return mcpProduct(id, "verified")
	}
	return mcpUnknown()
}
func mcpResolve(item todoItem, p priorityPolicy, policyID string) mcpResolution {
	var own struct {
		Project         *string         `json:"project"`
		ProductIdentity json.RawMessage `json:"product_identity"`
	}
	_ = json.Unmarshal(p.Items[policyID], &own)
	var explicit *mcpResolution
	for _, raw := range []json.RawMessage{own.ProductIdentity, item.ProductIdentity} {
		if len(raw) == 0 {
			continue
		}
		r := mcpExplicit(raw, p)
		if explicit != nil && *explicit != r {
			return mcpUnknown()
		}
		explicit = &r
	}
	declared := item.Project == "cc-pc-tools-rs" && (item.ID == 2641 || item.ID == 2737 || item.ID == 3093)
	if explicit != nil {
		if declared && explicit.id != "godd-mcp-alpha" {
			return mcpUnknown()
		}
		return *explicit
	}
	if declared {
		return mcpProduct("godd-mcp-alpha", "owner_declared")
	}
	project := item.Project
	if own.Project != nil {
		project = *own.Project
	} else if name, ok := optionalString(item.OwnerProject); ok {
		project = name
	}
	named := mcpNamed(project)
	if len(item.RepositoryID) > 0 || len(item.RepositoryOwnerID) > 0 {
		var repository uint64
		_ = json.Unmarshal(item.RepositoryID, &repository)
		known := named.scoped
		for _, id := range []string{"godd-mcp-contract", "godd-mcp-alpha"} {
			b, ok := mcpBinding(id, p)
			known = known || (ok && repository == b.Repository)
		}
		if known {
			raw, _ := json.Marshal(map[string]json.RawMessage{"repository_id": item.RepositoryID, "owner_id": item.RepositoryOwnerID})
			return mcpExplicit(raw, p)
		}
	}
	return named
}
func mcpCompatible(a, b mcpResolution) bool {
	return a.state != "unknown" && b.state != "unknown" && (a.id == "" || b.id == "" || a.id == b.id)
}
func (j jobMeta) productItem() todoItem {
	return todoItem{Project: j.Project, OwnerProject: j.OwnerProject, Department: j.Department, ProductIdentity: j.ProductIdentity, RepositoryID: j.RepositoryID, RepositoryOwnerID: j.RepositoryOwnerID}
}

// Match the shared Rust index's execution choice. Related IDs remain available
// for history, but a job occupies only its canonical primary's current slot.
func mcpPrimary(j jobMeta, ids []int64, items map[int64]todoItem) int64 {
	if j.TodoID > 0 {
		if _, exists := items[j.TodoID]; exists {
			for _, id := range ids {
				if id == j.TodoID {
					return j.TodoID
				}
			}
		}
		return 0
	}
	candidates, current, eligible := []int64{}, []int64{}, []int64{}
	for _, id := range positiveIDs(ids) {
		item, exists := items[id]
		if !exists {
			continue
		}
		candidates = append(candidates, id)
		if item.Scheduler.Job == j.ID {
			current = append(current, id)
		}
		if item.Status == "doing" || item.Status == "todo" {
			eligible = append(eligible, id)
		}
	}
	if len(current) > 0 {
		candidates = current
	} else if len(eligible) > 0 {
		candidates = eligible
	}
	if len(candidates) == 0 {
		return 0
	}
	return candidates[0]
}

func mcpRelations(j jobMeta, ids []int64, items map[int64]todoItem, p priorityPolicy) ([]int64, mcpResolution) {
	own := mcpResolve(j.productItem(), p, "")
	scoped := own.scoped
	for _, id := range ids {
		if i, ok := items[id]; ok {
			scoped = scoped || mcpResolve(i, p, strconv.FormatInt(id, 10)).scoped
		}
	}
	if !scoped {
		return ids, own
	}
	candidates := ids
	if j.TodoID > 0 {
		candidates = nil
		if _, ok := items[j.TodoID]; ok {
			candidates = []int64{j.TodoID}
		}
	} else {
		current := []int64{}
		for _, id := range candidates {
			if items[id].Scheduler.Job == j.ID {
				current = append(current, id)
			}
		}
		if len(current) > 0 {
			candidates = current
		}
	}
	if primary, ok := items[j.TodoID]; ok {
		target := mcpResolve(primary, p, strconv.FormatInt(j.TodoID, 10))
		if !mcpCompatible(own, target) {
			own = mcpUnknown()
		} else if !own.scoped {
			own = target
		}
	} else if !own.scoped {
		products := map[string]bool{}
		for _, id := range candidates {
			r := mcpResolve(items[id], p, strconv.FormatInt(id, 10))
			if r.state == "unknown" {
				own = mcpUnknown()
				break
			}
			if r.id != "" {
				products[r.id] = true
			}
		}
		if len(products) > 1 {
			own = mcpUnknown()
		}
	}
	if own.state == "unknown" {
		return []int64{}, own
	}
	if terminalStatus(j.Status) {
		history := []int64{}
		for _, id := range ids {
			r := mcpResolve(items[id], p, strconv.FormatInt(id, 10))
			if mcpCompatible(r, own) && !(j.TodoID > 0 && r.id != "" && own.id == "") {
				history = append(history, id)
			}
		}
		return positiveIDs(history), own
	}
	if j.TodoID == 0 {
		hasCurrent := false
		for _, id := range candidates {
			hasCurrent = hasCurrent || items[id].Scheduler.Job == j.ID
		}
		if !hasCurrent {
			eligible := []int64{}
			for _, id := range candidates {
				if items[id].Status == "doing" || items[id].Status == "todo" {
					eligible = append(eligible, id)
				}
			}
			if len(eligible) > 0 {
				candidates = eligible
			} else if own.id != "" {
				// An inactive legacy reference is history, not a current primary.
				candidates = nil
			}
		}
	}
	for _, id := range positiveIDs(candidates) {
		if mcpCompatible(mcpResolve(items[id], p, strconv.FormatInt(id, 10)), own) {
			return []int64{id}, own
		}
	}
	return []int64{}, own
}
